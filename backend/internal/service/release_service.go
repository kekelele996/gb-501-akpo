package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"sterile-packaging-release-control/backend/internal/constants"
	"sterile-packaging-release-control/backend/internal/dto"
	"sterile-packaging-release-control/backend/internal/model"
	"sterile-packaging-release-control/backend/internal/repository"
	"sterile-packaging-release-control/backend/internal/util"
)

type ReleaseService interface {
	List(context.Context, dto.PageQuery, string) (dto.PageResult[model.ReleaseDecision], error)
	Get(context.Context, uint) (*model.ReleaseDecision, error)
	Decide(context.Context, Actor, dto.CreateReleaseDecisionRequest) (*model.ReleaseDecision, error)
}

type releaseService struct {
	repo           repository.ReleaseRepository
	batchRepo      repository.BatchRepository
	inspectionRepo repository.InspectionRepository
	audit          AuditService
	tx             repository.Transactor
}

func NewReleaseService(repo repository.ReleaseRepository, batchRepo repository.BatchRepository, inspectionRepo repository.InspectionRepository, audit AuditService, tx repository.Transactor) ReleaseService {
	return &releaseService{repo: repo, batchRepo: batchRepo, inspectionRepo: inspectionRepo, audit: audit, tx: tx}
}

func (s *releaseService) List(ctx context.Context, query dto.PageQuery, decision string) (dto.PageResult[model.ReleaseDecision], error) {
	query = query.Normalize()
	items, total, err := s.repo.List(ctx, query, decision)
	return dto.PageResult[model.ReleaseDecision]{Items: items, Total: total, Page: query.Page, PageSize: query.PageSize}, err
}

func (s *releaseService) Get(ctx context.Context, id uint) (*model.ReleaseDecision, error) {
	return s.repo.Find(ctx, id)
}

func (s *releaseService) Decide(ctx context.Context, actor Actor, input dto.CreateReleaseDecisionRequest) (*model.ReleaseDecision, error) {
	if !input.Decision.Valid() {
		return nil, util.BadRequest("无效的放行决定")
	}
	var decision *model.ReleaseDecision
	err := s.tx.WithinTransaction(ctx, func(txCtx context.Context) error {
		batch, err := s.batchRepo.FindForUpdate(txCtx, input.ProductionBatchID)
		if err != nil {
			return err
		}
		if batch.Status == constants.BatchStatusDraft {
			return util.Conflict("草稿批次不能审批")
		}
		if batch.Status == constants.BatchStatusReleased {
			return util.Conflict("批次已经放行")
		}
		// 隔离与返工不受三段覆盖限制，只做批次状态校验；放行必须满足全部检验红线。
		var incomplete, failed int64
		if input.Decision == constants.DecisionRelease {
			_, missingSegments, requestedRetest := batch.SegmentCoverage()
			if len(batch.Inspections) == 0 || len(missingSegments) > 0 {
				labels := make([]string, 0, len(missingSegments))
				for _, segment := range missingSegments {
					labels = append(labels, segment.ShortLabel())
				}
				if len(batch.Inspections) == 0 {
					return util.Conflict("批次尚无检验记录，放行要求起始段、中段、末段均有已完成且合格的样本")
				}
				return util.Conflict(fmt.Sprintf("放行要求起始段、中段、末段均有已完成且合格的样本，缺少：%s", strings.Join(labels, "、")))
			}
			if requestedRetest > 0 {
				return util.Conflict("仍有待复测的检验，不能放行")
			}
			var countErr error
			incomplete, countErr = s.inspectionRepo.CountIncomplete(txCtx, batch.ID)
			if countErr != nil {
				return countErr
			}
			failed, countErr = s.inspectionRepo.CountByResult(txCtx, batch.ID, "fail")
			if countErr != nil {
				return countErr
			}
			if incomplete > 0 {
				return util.Conflict("仍有待完成或待复测的检验")
			}
			if failed > 0 {
				return util.Conflict("存在不合格检验，不能放行")
			}
		}
		before := *batch
		switch input.Decision {
		case constants.DecisionRelease:
			batch.Status = constants.BatchStatusReleased
			now := time.Now()
			batch.CompletedAt = &now
		case constants.DecisionQuarantine:
			batch.Status = constants.BatchStatusHold
			batch.HoldReason = strings.TrimSpace(input.Reason)
		case constants.DecisionRework:
			batch.Status = constants.BatchStatusRework
			batch.HoldReason = strings.TrimSpace(input.Reason)
		}
		_, missingSegments, requestedRetest := batch.SegmentCoverage()
		coverage := fmt.Sprintf("三段合格覆盖 %d/3", len(constants.AllSegments())-len(missingSegments))
		if len(missingSegments) > 0 {
			coverage += "（缺：" + strings.Join(batch.MissingSegmentLabels(), "、") + "）"
		}
		decision = &model.ReleaseDecision{
			ProductionBatchID: batch.ID, Decision: input.Decision, ApproverID: actor.ID,
			ApproverName: actor.Name, Reason: strings.TrimSpace(input.Reason), EffectiveAt: time.Now(),
			InspectionSummary: fmt.Sprintf("共 %d 项检验，%d 项不合格，%d 项待处理，%d 项待复测；%s", len(batch.Inspections), failed, incomplete, requestedRetest, coverage),
		}
		decision.Normalize()
		if err := decision.Validate(); err != nil {
			return util.BadRequest(err.Error())
		}
		if err := s.repo.CreateWithBatch(txCtx, decision, batch); err != nil {
			return err
		}
		return s.audit.Record(txCtx, actor, "release.decided", "ProductionBatch", batch.ID, before, batch)
	})
	if err != nil {
		return nil, err
	}
	return s.repo.Find(ctx, decision.ID)
}

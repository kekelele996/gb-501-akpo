import { Alert, Button, Descriptions, Form, Input, Modal, Segmented, Space, Typography } from 'antd'
import { CheckCircleFilled, CheckCircleOutlined, MinusCircleOutlined, StopOutlined, ToolOutlined } from '@ant-design/icons'
import { useEffect, useMemo, useState } from 'react'
import type { DecisionType, ProductionBatch, SampleSegment } from '../../types/domain'
import { SEGMENT_LABELS, SEGMENT_ORDER, segmentCoverage } from '../../utils/segment'
import { BatchStatusBadge } from './BatchStatusBadge'

interface Props {
  batch: ProductionBatch
  canDecide: boolean
  loading?: boolean
  onDecide: (decision: DecisionType, reason: string) => Promise<void>
}

export function DecisionPanel({ batch, canDecide, loading, onDecide }: Props) {
  const [open, setOpen] = useState(false)
  const [decision, setDecision] = useState<DecisionType>('release')
  const [form] = Form.useForm<{ reason: string }>()
  const coverage = useMemo(() => segmentCoverage(batch.inspections), [batch.inspections])
  const releaseBlocked = coverage.missing.length > 0 || coverage.retest > 0 || (batch.inspections?.length || 0) === 0
  const missingText = coverage.missing.map((segment) => SEGMENT_LABELS[segment]).join('、')

  // 每次打开弹窗或覆盖状态变化时，保证停留在当前允许的决定上。
  useEffect(() => {
    if (open && decision === 'release' && releaseBlocked) setDecision('quarantine')
  }, [open, releaseBlocked, decision])

  const submit = async () => {
    const values = await form.validateFields()
    await onDecide(decision, values.reason)
    setOpen(false)
    form.resetFields()
  }

  const renderSegmentItem = (segment: SampleSegment) => {
    const covered = coverage.passed.includes(segment)
    return (
      <Space size={4}>
        {covered
          ? <CheckCircleFilled style={{ color: '#52c41a' }} />
          : <MinusCircleOutlined style={{ color: '#ff4d4f' }} />}
        <span style={{ color: covered ? undefined : '#ff4d4f' }}>{SEGMENT_LABELS[segment]}</span>
      </Space>
    )
  }

  return (
    <section className="decision-panel">
      <div className="panel-heading"><div><Typography.Title level={4}>放行判定</Typography.Title><Typography.Text type="secondary">{batch.batchNo}</Typography.Text></div><BatchStatusBadge status={batch.status} /></div>
      <Descriptions column={2} size="small">
        <Descriptions.Item label="检验总数">{batch.inspections?.length || 0}</Descriptions.Item>
        <Descriptions.Item label="待复测">{coverage.retest}</Descriptions.Item>
        <Descriptions.Item label="三段覆盖" span={2}>
          <Space size="large" wrap>
            {SEGMENT_ORDER.map((segment) => (
              <span key={segment}>{renderSegmentItem(segment)}</span>
            ))}
            <Typography.Text strong>{coverage.passed.length}/3</Typography.Text>
          </Space>
        </Descriptions.Item>
        <Descriptions.Item label="责任班组">{batch.responsibleTeam}</Descriptions.Item>
      </Descriptions>
      {coverage.missing.length > 0 && (
        <Alert
          className="panel-alert"
          type="error"
          showIcon
          message={`放行要求三段均有已完成且合格的样本，缺少：${missingText}`}
        />
      )}
      {coverage.missing.length === 0 && coverage.retest > 0 && (
        <Alert className="panel-alert" type="warning" showIcon message="三段均已覆盖，但仍有待复测样本，处理完成前不能放行" />
      )}
      <Button type="primary" disabled={!canDecide || batch.status === 'released'} onClick={() => setOpen(true)}>提交决定</Button>
      <Modal title={`审批批次 ${batch.batchNo}`} open={open} confirmLoading={loading} onOk={() => void submit()} onCancel={() => setOpen(false)} okText="确认提交" cancelText="取消">
        <Space direction="vertical" size="large" style={{ width: '100%' }}>
          <Segmented
            block
            value={decision}
            onChange={(value) => setDecision(value as DecisionType)}
            options={[
              { label: '放行', value: 'release', icon: <CheckCircleOutlined />, disabled: releaseBlocked },
              { label: '隔离', value: 'quarantine', icon: <StopOutlined /> },
              { label: '返工', value: 'rework', icon: <ToolOutlined /> },
            ]}
          />
          {decision === 'release' && releaseBlocked && (
            <Alert type="error" showIcon message={coverage.missing.length > 0 ? `缺少合格段位：${missingText}，已禁用放行` : '存在待复测样本，已禁用放行'} />
          )}
          {decision !== 'release' && (
            <Alert type="info" showIcon message="隔离和返工不受三段覆盖限制，但必须填写理由。" />
          )}
          <Form form={form} layout="vertical"><Form.Item label="审批理由" name="reason" rules={[{ required: true, min: 5, message: '请填写至少 5 个字的审批理由' }]}><Input.TextArea rows={4} maxLength={1000} showCount /></Form.Item></Form>
        </Space>
      </Modal>
    </section>
  )
}

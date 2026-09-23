import { Alert, Button, Descriptions, Form, Input, Modal, Segmented, Space, Tag, Typography } from 'antd'
import { CheckCircleFilled, CheckCircleOutlined, MinusCircleOutlined, StopOutlined, ToolOutlined } from '@ant-design/icons'
import { useState } from 'react'
import type { DecisionType, ProductionBatch, SampleSegment } from '../../types/domain'
import { BatchStatusBadge } from './BatchStatusBadge'
import { SEGMENT_LABELS, SEGMENT_ORDER, hasRequestedRetest, missingSegments, segmentCoverage } from '../../utils/segment'

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
  const samples = batch.inspections || []
  const pending = samples.filter((item) => item.result === 'pending' || item.retestStatus === 'requested').length
  const failed = samples.filter((item) => item.result === 'fail').length
  const awaitingRetest = hasRequestedRetest(samples)
  const coverage = segmentCoverage(samples)
  const missing: SampleSegment[] = missingSegments(samples)
  const releaseBlocked = pending > 0 || awaitingRetest || missing.length > 0
  const submit = async () => {
    const values = await form.validateFields()
    await onDecide(decision, values.reason)
    setOpen(false)
    form.resetFields()
  }
  return (
    <section className="decision-panel">
      <div className="panel-heading"><div><Typography.Title level={4}>放行判定</Typography.Title><Typography.Text type="secondary">{batch.batchNo}</Typography.Text></div><BatchStatusBadge status={batch.status} /></div>
      <Descriptions column={2} size="small">
        <Descriptions.Item label="检验总数">{samples.length}</Descriptions.Item>
        <Descriptions.Item label="待处理">{pending}</Descriptions.Item>
        <Descriptions.Item label="不合格">{failed}</Descriptions.Item>
        <Descriptions.Item label="责任班组">{batch.responsibleTeam}</Descriptions.Item>
        <Descriptions.Item label="三段合格覆盖" span={2}>
          <Space size={4} wrap>
            {SEGMENT_ORDER.map((segment) => coverage[segment]
              ? <Tag key={segment} icon={<CheckCircleFilled />} color="success">{SEGMENT_LABELS[segment]}</Tag>
              : <Tag key={segment} icon={<MinusCircleOutlined />}>{SEGMENT_LABELS[segment]}</Tag>)}
          </Space>
        </Descriptions.Item>
      </Descriptions>
      {missing.length > 0 && <Alert className="panel-alert" type="warning" showIcon message={`放行前以下段位缺少已完成且合格的样本：${missing.map((segment) => SEGMENT_LABELS[segment]).join('、')}`} />}
      {awaitingRetest && <Alert className="panel-alert" type="warning" showIcon message="存在待复测样本，复测闭环前不能放行" />}
      {pending > 0 && !awaitingRetest && <Alert className="panel-alert" type="warning" showIcon message={`当前有 ${pending} 项待完成检验，不能直接放行`} />}
      <Space wrap>
        <Button type="primary" icon={<CheckCircleOutlined />} disabled={!canDecide || batch.status === 'released' || releaseBlocked} title={releaseBlocked ? '三段未全部合格或仍有待复测' : undefined} onClick={() => { setDecision('release'); setOpen(true) }}>放行</Button>
        <Button danger icon={<StopOutlined />} disabled={!canDecide || batch.status === 'released'} onClick={() => { setDecision('quarantine'); setOpen(true) }}>隔离</Button>
        <Button icon={<ToolOutlined />} disabled={!canDecide || batch.status === 'released'} onClick={() => { setDecision('rework'); setOpen(true) }}>返工</Button>
        {!canDecide && <Typography.Text type="secondary">您没有放行决定权限</Typography.Text>}
      </Space>
      <Modal title={`审批批次 ${batch.batchNo}`} open={open} confirmLoading={loading} onOk={() => void submit()} onCancel={() => setOpen(false)} okText="确认提交" cancelText="取消">
        <Space direction="vertical" size="large" style={{ width: '100%' }}>
          {releaseBlocked && decision === 'release' && <Alert type="error" showIcon message={missing.length > 0 ? `缺少合格段位：${missing.map((segment) => SEGMENT_LABELS[segment]).join('、')}` : '仍有待完成检验或待复测样本'} description="放行已被禁用，可改选隔离或返工并填写理由" />}
          <Segmented block value={decision} onChange={(value) => setDecision(value as DecisionType)} options={[
            { label: '放行', value: 'release', icon: <CheckCircleOutlined />, disabled: releaseBlocked },
            { label: '隔离', value: 'quarantine', icon: <StopOutlined /> },
            { label: '返工', value: 'rework', icon: <ToolOutlined /> },
          ]} />
          <Form form={form} layout="vertical"><Form.Item label="审批理由" name="reason" rules={[{ required: true, message: '请填写审批理由' }, { min: 5, message: '请填写至少 5 个字的审批理由' }]}><Input.TextArea rows={4} maxLength={1000} showCount /></Form.Item></Form>
        </Space>
      </Modal>
    </section>
  )
}

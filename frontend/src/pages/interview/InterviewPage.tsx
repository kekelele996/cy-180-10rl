// 采访工作台：选择项目 → 按问题录音 → 自动关联 → 一句话摘要 → 时间轴标注 → 人工排序。
import { useCallback, useEffect, useRef, useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import AudioPlayer from '../../components/AudioPlayer'
import ConfirmDialog from '../../components/ConfirmDialog'
import EmptyState from '../../components/EmptyState'
import StatusBadge from '../../components/StatusBadge'
import { useProjectStore } from '../../stores/projectStore'
import { useQuestionStore } from '../../stores/questionStore'
import { useRecordingStore } from '../../stores/recordingStore'
import { useTimelineStore } from '../../stores/timelineStore'
import { formatDuration } from '../../utils/format'
import type { Recording } from '../../api/types'

export default function InterviewPage() {
  const [params, setParams] = useSearchParams()
  const selectedProject = Number(params.get('project_id')) || 0
  const { projects, fetchList } = useProjectStore()
  const { questions, fetchByProject } = useQuestionStore()
  const { fetchByProject: fetchRecordings } = useRecordingStore()
  const [activeQuestion, setActiveQuestion] = useState(0)
  const [message, setMessage] = useState('')

  useEffect(() => {
    fetchList({ page: 1, page_size: 100 })
  }, [fetchList])

  useEffect(() => {
    if (selectedProject) {
      fetchByProject(selectedProject)
      fetchRecordings(selectedProject)
      setActiveQuestion(0)
    } else {
      setActiveQuestion(0)
    }
  }, [selectedProject, fetchByProject, fetchRecordings])

  const chooseProject = (projectId: number) => {
    const next = new URLSearchParams(params)
    if (projectId) {
      next.set('project_id', String(projectId))
    } else {
      next.delete('project_id')
    }
    setParams(next)
  }

  return (
    <div className="page">
      <div className="page-header">
        <h2>采访工作台</h2>
      </div>
      {message && <div className="toast success">{message}</div>}

      <section className="card">
        <div className="card-title">选择采访项目</div>
        <select value={selectedProject} onChange={(e) => chooseProject(Number(e.target.value))}>
          <option value={0}>请选择项目</option>
          {projects.map((p) => (
            <option key={p.id} value={p.id}>
              {p.title}（{p.interviewee_name}）
            </option>
          ))}
        </select>
      </section>

      {selectedProject === 0 ? (
        <EmptyState title="请先选择采访项目" description="选择一个项目后即可开始录音" />
      ) : (
        <>
          <section className="card">
            <div className="card-title">采访问题列表</div>
            {questions.length === 0 ? (
              <EmptyState title="该项目还没有采访问题" description="请先到项目详情页添加采访问题" />
            ) : (
              <div className="question-tabs">
                {questions.map((q, idx) => (
                  <button
                    key={q.id}
                    className={`question-tab ${activeQuestion === q.id ? 'active' : ''}`}
                    onClick={() => setActiveQuestion(q.id)}
                  >
                    {idx + 1}. {q.content}
                  </button>
                ))}
              </div>
            )}
          </section>

          {activeQuestion > 0 && (
            <RecorderPanel
              projectId={selectedProject}
              questionId={activeQuestion}
              onRecorded={(summary) => {
                fetchRecordings(selectedProject)
                setMessage(summary)
                setTimeout(() => setMessage(''), 4000)
              }}
              onOrderSaved={() => fetchRecordings(selectedProject)}
            />
          )}
        </>
      )}
    </div>
  )
}

function RecorderPanel({
  projectId,
  questionId,
  onRecorded,
  onOrderSaved,
}: {
  projectId: number
  questionId: number
  onRecorded: (msg: string) => void
  onOrderSaved: () => void
}) {
  const {
    create,
    uploadAudio,
    updateSummary,
    remove: removeRecording,
    reorderByQuestion,
    fetchByQuestion,
  } = useRecordingStore()
  const { markers, fetchByRecording, create: createMarker } = useTimelineStore()
  const [recording, setRecording] = useState(false)
  const [seconds, setSeconds] = useState(0)
  const [uploading, setUploading] = useState(0)
  const [recordings, setRecordings] = useState<Recording[]>([])
  // 人工排序草稿：进入排序后，仅在本问题片段内调整顺序；保存成功前不影响实际收听顺序。
  const [orderDraft, setOrderDraft] = useState<number[] | null>(null)
  const [savingOrder, setSavingOrder] = useState(false)
  const mediaRecorderRef = useRef<MediaRecorder | null>(null)
  const chunksRef = useRef<Blob[]>([])
  const timerRef = useRef<number | null>(null)

  const reload = useCallback(async () => {
    const list = await fetchByQuestion(questionId)
    setRecordings(list)
    // 排序进行中若发生并发变动：保留草稿中仍存在的片段顺序，新录音接在末尾，被移除的片段自动剔除。
    setOrderDraft((prev) => {
      if (prev === null) return null
      const liveIds = new Set(list.map((r) => r.id))
      const kept = prev.filter((id) => liveIds.has(id))
      const appended = list.filter((r) => !prev.includes(r.id)).map((r) => r.id)
      return [...kept, ...appended]
    })
  }, [fetchByQuestion, questionId])

  useEffect(() => {
    reload()
  }, [reload])

  // 切换问题时丢弃未保存的排序草稿，绝不会把别的问题片段带入。
  useEffect(() => {
    setOrderDraft(null)
  }, [questionId])

  useEffect(() => {
    if (recording) {
      timerRef.current = window.setInterval(() => setSeconds((s) => s + 1), 1000)
    } else if (timerRef.current) {
      window.clearInterval(timerRef.current)
      timerRef.current = null
    }
    return () => {
      if (timerRef.current) window.clearInterval(timerRef.current)
    }
  }, [recording])

  const startRecording = async () => {
    try {
      const stream = await navigator.mediaDevices.getUserMedia({ audio: true })
      const recorder = new MediaRecorder(stream)
      chunksRef.current = []
      recorder.ondataavailable = (e) => {
        if (e.data.size > 0) chunksRef.current.push(e.data)
      }
      recorder.onstop = async () => {
        stream.getTracks().forEach((t) => t.stop())
        const blob = new Blob(chunksRef.current, { type: 'audio/webm' })
        setRecording(false)
        setUploading(1)
        try {
          const created = await create({ project_id: projectId, question_id: questionId, duration_seconds: seconds })
          await uploadAudio(created.id, blob, seconds, (p) => setUploading(p))
          await reload()
          onRecorded('录音上传成功，已自动关联到当前问题')
        } catch (e) {
          onRecorded(e instanceof Error ? e.message : '录音上传失败')
        } finally {
          setUploading(0)
          setSeconds(0)
        }
      }
      mediaRecorderRef.current = recorder
      recorder.start()
      setRecording(true)
      setSeconds(0)
    } catch {
      alert('无法访问麦克风，请在浏览器中授权麦克风权限')
    }
  }

  const stopRecording = () => {
    mediaRecorderRef.current?.stop()
  }

  const addMarker = async (recordingId: number, label: string) => {
    await createMarker({
      project_id: projectId,
      recording_id: recordingId,
      timestamp_second: 0,
      label,
    })
    await fetchByRecording(recordingId)
  }

  const moveDraft = (index: number, delta: -1 | 1) => {
    if (!orderDraft) return
    const target = index + delta
    if (target < 0 || target >= orderDraft.length) return
    const next = [...orderDraft]
    ;[next[index], next[target]] = [next[target], next[index]]
    setOrderDraft(next)
  }

  const saveOrder = async () => {
    if (!orderDraft) return
    setSavingOrder(true)
    try {
      const list = await reorderByQuestion(questionId, orderDraft)
      setRecordings(list)
      setOrderDraft(null)
      onOrderSaved()
      onRecorded('录音顺序已保存，采访工作台与项目时间线均按新顺序播放')
    } catch (e) {
      onRecorded(e instanceof Error ? e.message : '保存录音顺序失败')
      await reload()
    } finally {
      setSavingOrder(false)
    }
  }

  const cancelOrder = () => setOrderDraft(null)

  const removeAt = async (recordingId: number) => {
    await removeRecording(recordingId)
    // 移除片段后只重拉本问题：其余片段的相对顺序保持稳定，不影响其他问题。
    await reload()
    onOrderSaved()
    onRecorded('录音片段已移除，其余片段顺序保持不变')
  }

  // 进入排序或调整过程中始终以草稿顺序展示；非排序态以服务端收听顺序展示。
  const orderedRecordings = orderDraft
    ? orderDraft
        .map((id) => recordings.find((r) => r.id === id))
        .filter((r): r is Recording => Boolean(r))
    : recordings
  const orderDirty =
    orderDraft !== null && orderDraft.some((id, idx) => recordings[idx]?.id !== id)

  return (
    <section className="card">
      <div className="card-title">录音面板</div>
      <div className="recorder-box">
        {uploading > 0 ? (
          <div className="upload-progress">
            上传中… {uploading}%
            <div className="progress-bar">
              <div className="progress-inner" style={{ width: `${uploading}%` }} />
            </div>
          </div>
        ) : recording ? (
          <>
            <div className="recording-indicator">
              <span className="rec-dot" /> 正在录音 {formatDuration(seconds)}
            </div>
            <button className="btn btn-danger" onClick={stopRecording}>
              ⏹ 停止并保存
            </button>
          </>
        ) : (
          <button className="btn btn-primary" onClick={startRecording}>
            ⏺ 开始录音
          </button>
        )}
      </div>

      <div className="card-title" style={{ marginTop: 20, display: 'flex', alignItems: 'center', gap: 10 }}>
        <span>本问题已录片段（{orderedRecordings.length}）</span>
        {orderDraft === null ? (
          <button
            className="btn btn-plain btn-small"
            disabled={recordings.length < 2}
            onClick={() => setOrderDraft(recordings.map((r) => r.id))}
          >
            ↕ 人工排序
          </button>
        ) : (
          <div className="order-actions">
            <span className="muted">拖动前可点上移/下移，调整后请保存</span>
            <button className="btn btn-primary btn-small" disabled={!orderDirty || savingOrder} onClick={saveOrder}>
              {savingOrder ? '保存中…' : '保存顺序'}
            </button>
            <button className="btn btn-plain btn-small" disabled={savingOrder} onClick={cancelOrder}>
              取消
            </button>
          </div>
        )}
      </div>
      {orderedRecordings.length === 0 ? (
        <EmptyState title="还没有录音" description="点击上方开始录音，新片段会自动接在本问题末尾" />
      ) : (
        <div className="recording-list">
          {orderedRecordings.map((r, idx) => (
            <div key={r.id} className={`recording-row ${orderDraft !== null ? 'order-editing' : ''}`}>
              <div className="recording-meta">
                <span className="recording-order-no">{idx + 1}</span>
                <StatusBadge status={r.status} type="recording" />
                <span className="muted">{formatDuration(r.duration_seconds)}</span>
                {orderDraft !== null && (
                  <div className="order-buttons">
                    <button
                      className="btn btn-plain btn-small"
                      disabled={idx === 0 || savingOrder}
                      onClick={() => moveDraft(idx, -1)}
                      title="上移"
                    >
                      ↑ 上移
                    </button>
                    <button
                      className="btn btn-plain btn-small"
                      disabled={idx === orderDraft.length - 1 || savingOrder}
                      onClick={() => moveDraft(idx, 1)}
                      title="下移"
                    >
                      ↓ 下移
                    </button>
                    <ConfirmDialog
                      title="移除录音片段"
                      message="移除该片段后，本问题其余片段的相对顺序保持不变，确定移除？"
                      danger
                      confirmText="移除"
                      onConfirm={() => removeAt(r.id)}
                    >
                      <button className="btn btn-plain btn-small" disabled={savingOrder}>
                        移除
                      </button>
                    </ConfirmDialog>
                  </div>
                )}
              </div>
              <AudioPlayer recordingId={r.id} durationSeconds={r.duration_seconds} />
              <SummaryEditor recording={r} onSave={updateSummary} onSaved={reload} />
              <div className="marker-actions">
                <span className="muted">时间轴节点：</span>
                {markers
                  .filter((m) => m.recording_id === r.id)
                  .map((m) => (
                    <span key={m.id} className="marker-chip">
                      {m.label}
                    </span>
                  ))}
                <input
                  placeholder="新增节点，如：讲到参军经历"
                  style={{ maxWidth: 220 }}
                  id={`marker-input-${r.id}`}
                />
                <button
                  className="btn btn-plain btn-small"
                  onClick={() => {
                    const input = document.getElementById(`marker-input-${r.id}`) as HTMLInputElement
                    if (input?.value.trim()) {
                      addMarker(r.id, input.value.trim())
                      input.value = ''
                    }
                  }}
                >
                  ＋ 标注
                </button>
              </div>
            </div>
          ))}
        </div>
      )}
    </section>
  )
}

// SummaryEditor 每行独立的一句话摘要编辑框，避免共享草稿造成跨行串显。
function SummaryEditor({
  recording,
  onSave,
  onSaved,
}: {
  recording: Recording
  onSave: (id: number, summary: string) => Promise<void>
  onSaved: () => Promise<void> | void
}) {
  const [draft, setDraft] = useState('')
  return (
    <div className="summary-edit">
      <input
        value={draft || recording.summary}
        placeholder="写一句话摘要"
        onChange={(e) => setDraft(e.target.value)}
      />
      <button
        className="btn btn-plain btn-small"
        disabled={!draft.trim()}
        onClick={async () => {
          await onSave(recording.id, draft.trim())
          setDraft('')
          await onSaved()
        }}
      >
        保存摘要
      </button>
    </div>
  )
}

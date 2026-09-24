// 录音片段状态管理。
import { create } from 'zustand'
import {
  createRecording,
  deleteRecording,
  listRecordings,
  reorderQuestionRecordings,
  updateRecordingSummary,
  uploadRecordingAudio,
} from '../api/recording'
import type { Recording } from '../api/types'

interface RecordingState {
  recordings: Recording[]
  loading: boolean
  fetchByProject: (projectId: number) => Promise<void>
  fetchByQuestion: (questionId: number) => Promise<Recording[]>
  create: (payload: { project_id: number; question_id: number; duration_seconds?: number }) => Promise<Recording>
  uploadAudio: (id: number, blob: Blob, duration: number, onProgress?: (p: number) => void) => Promise<void>
  updateSummary: (id: number, summary: string) => Promise<void>
  reorder: (questionId: number, recordingIds: number[]) => Promise<Recording[]>
  remove: (id: number) => Promise<void>
}

export const useRecordingStore = create<RecordingState>((set) => ({
  recordings: [],
  loading: false,

  async fetchByProject(projectId) {
    set({ loading: true })
    try {
      const res = await listRecordings({ project_id: projectId })
      set({ recordings: res.list, loading: false })
    } catch (e) {
      console.error('fetch recordings failed', e)
      set({ loading: false })
    }
  },

  async fetchByQuestion(questionId) {
    const res = await listRecordings({ question_id: questionId })
    return res.list
  },

  async create(payload) {
    const recording = await createRecording(payload)
    set((s) => ({ recordings: [...s.recordings, recording] }))
    return recording
  },

  async uploadAudio(id, blob, duration, onProgress) {
    const updated = await uploadRecordingAudio(id, blob, duration, onProgress)
    set((s) => ({ recordings: s.recordings.map((r) => (r.id === id ? updated : r)) }))
  },

  async updateSummary(id, summary) {
    const updated = await updateRecordingSummary(id, summary)
    set((s) => ({ recordings: s.recordings.map((r) => (r.id === id ? updated : r)) }))
  },

  async reorder(questionId, recordingIds) {
    const res = await reorderQuestionRecordings(questionId, recordingIds)
    // 把最新的 sort_order 同步到项目级列表，项目时间线随之按新顺序播放。
    set((s) => ({
      recordings: s.recordings
        .map((r) => {
          const updated = res.list.find((x) => x.id === r.id)
          return updated ? { ...r, sort_order: updated.sort_order } : r
        })
        .sort((a, b) => a.question_id - b.question_id || a.sort_order - b.sort_order || a.id - b.id),
    }))
    return res.list
  },

  async remove(id) {
    await deleteRecording(id)
    set((s) => ({ recordings: s.recordings.filter((r) => r.id !== id) }))
  },
}))

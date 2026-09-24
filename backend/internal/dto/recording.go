package dto

// CreateRecordingRequest 创建录音记录请求。
type CreateRecordingRequest struct {
	ProjectID       uint   `json:"project_id" binding:"required"`
	QuestionID      uint   `json:"question_id" binding:"required"`
	DurationSeconds int    `json:"duration_seconds" binding:"omitempty,min=0"`
	Summary         string `json:"summary" binding:"omitempty,max=512"`
}

// UpdateRecordingRequest 更新录音信息请求。
type UpdateRecordingRequest struct {
	DurationSeconds int    `json:"duration_seconds" binding:"omitempty,min=0"`
	Summary         string `json:"summary" binding:"omitempty,max=512"`
	Status          string `json:"status" binding:"omitempty,oneof=recording processing ready failed"`
}

// ReorderRecordingsRequest 保存某问题下录音人工播放顺序的请求，
// recording_ids 按目标播放顺序排列；未包含的录音按原相对顺序顺延，不允许混入其他问题的录音。
type ReorderRecordingsRequest struct {
	RecordingIDs []uint `json:"recording_ids" binding:"required,min=1,dive,gt=0"`
}

// RecordingResponse 录音响应。
type RecordingResponse struct {
	ID              uint   `json:"id"`
	ProjectID       uint   `json:"project_id"`
	QuestionID      uint   `json:"question_id"`
	AudioKey        string `json:"audio_key"`
	DurationSeconds int    `json:"duration_seconds"`
	Summary         string `json:"summary"`
	Status          string `json:"status"`
	CreatedBy       uint   `json:"created_by"`
	CreatedAt       string `json:"created_at"`
	UpdatedAt       string `json:"updated_at"`
}

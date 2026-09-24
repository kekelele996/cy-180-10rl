// Package repository 负责数据访问层。
package repository

import "errors"

// 仓储层哨兵错误。
var (
	ErrNotFound      = errors.New("record not found")
	ErrConflict      = errors.New("record conflict")
	ErrDuplicateName = errors.New("duplicate name")
	// ErrOrderMismatch 提交的录音集合与该问题当前片段集合不一致（有新增/删除混入），拒绝重排。
	ErrOrderMismatch = errors.New("recording order set mismatch")
)

package router

import (
	"log/slog"

	"github.com/gin-gonic/gin"
	"github.com/oralhistory/oralhistory/internal/config"
	"github.com/oralhistory/oralhistory/internal/handler"
	"github.com/oralhistory/oralhistory/internal/middleware"
)

// RegisterRecordingRoutes 注册录音路由。
func RegisterRecordingRoutes(g *gin.RouterGroup, h *handler.RecordingHandler, cfg *config.Config, logger *slog.Logger) {
	// 录音人工排序以问题为作用域，路径挂在 questions 下，与问题级查询保持一致。
	g.PUT("/questions/:id/recordings/reorder",
		middleware.Auth(cfg.JWTSecret, logger), h.Reorder)

	group := g.Group("/recordings", middleware.Auth(cfg.JWTSecret, logger))
	{
		group.GET("", h.List)
		group.POST("", h.Create)
		group.GET("/:id", h.Get)
		group.PUT("/:id", h.Update)
		group.PUT("/:id/summary", h.UpdateSummary)
		group.POST("/:id/audio", h.UploadAudio)
		group.GET("/:id/audio", h.PlayAudio)
		group.DELETE("/:id", h.Delete)
	}
}

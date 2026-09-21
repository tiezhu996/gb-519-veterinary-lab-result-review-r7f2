package handler

import (
	"net/http"

	"github.com/blueship581/veterinary-lab-result-review/backend/internal/dto"
	"github.com/blueship581/veterinary-lab-result-review/backend/internal/middleware"
	"github.com/blueship581/veterinary-lab-result-review/backend/internal/service"
	"github.com/blueship581/veterinary-lab-result-review/backend/internal/util"
	"github.com/gin-gonic/gin"
)

type CriticalDispositionHandler struct {
	service service.CriticalDispositionService
}

func NewCriticalDispositionHandler(s service.CriticalDispositionService) *CriticalDispositionHandler {
	return &CriticalDispositionHandler{service: s}
}

func (h *CriticalDispositionHandler) Register(group *gin.RouterGroup) {
	resource := group.Group("/dispositions")
	resource.GET("", h.list)
	resource.GET("/:id", h.get)
	resource.POST("/:id/confirm", middleware.RequireRoles("reviewer", "admin"), h.confirm)
}

func (h *CriticalDispositionHandler) list(c *gin.Context) {
	query := bindPage(c)
	result, err := h.service.List(c.Request.Context(), query)
	if err != nil {
		handleError(c, err)
		return
	}
	util.Page(c, result.Items, result.Page, result.PageSize, result.Total)
}

func (h *CriticalDispositionHandler) get(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	item, err := h.service.Get(c.Request.Context(), id)
	if err != nil {
		handleError(c, err)
		return
	}
	util.OK(c, item)
}

func (h *CriticalDispositionHandler) confirm(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	var input dto.ConfirmCriticalDisposition
	if err := c.ShouldBindJSON(&input); err != nil {
		util.Fail(c, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	item, err := h.service.Confirm(c.Request.Context(), id, input, actorFromContext(c), roleFromContext(c), requestIDFromContext(c))
	if err != nil {
		handleError(c, err)
		return
	}
	util.OK(c, item)
}

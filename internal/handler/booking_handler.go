package handler

import (
	"errors"
	"github.com/Aotorun/ticketflow/internal/domain"
	"github.com/Aotorun/ticketflow/internal/service"
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

const holdTTL = 5 * time.Minute

type reserveRequest struct {
	UserID uuid.UUID `json:"user_id" binding:"required"`
}

type BookingHandler struct {
	svc *service.BookingService
}

func NewBookingHandler(svc *service.BookingService) *BookingHandler {
	return &BookingHandler{svc}
}

func (h *BookingHandler) GetSeatMap(c *gin.Context) {
	idStr := c.Param("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid event id"})
		return
	}

	res, err := h.svc.GetSeatMap(c.Request.Context(), id)
	if err != nil {
		log.Print(err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}

	c.Data(http.StatusOK, "application/json; charset=utf-8", []byte(res))
}

func (h *BookingHandler) ReserveAny(c *gin.Context) {
	idStr := c.Param("id")
	eventID, err := uuid.Parse(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid event id"})
		return
	}

	var req reserveRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}

	seatID, err := h.svc.ReserveAny(c.Request.Context(), eventID, req.UserID, holdTTL)
	if err != nil {
		c.JSON(handlingError(err))
		return
	}

	c.JSON(http.StatusOK, gin.H{"seat_id": seatID})
}

func (h *BookingHandler) ReserveSpecific(c *gin.Context) {
	idStr := c.Param("id")
	eventID, err := uuid.Parse(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid event id"})
		return
	}

	idSeatStr := c.Param("seat_id")
	seatID, err := uuid.Parse(idSeatStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid seat id"})
		return
	}

	var req reserveRequest
	err = c.ShouldBindJSON(&req)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}

	err = h.svc.ReserveSpecific(c.Request.Context(), eventID, seatID, req.UserID, holdTTL)
	if err != nil {
		c.JSON(handlingError(err))
		return
	}

	c.JSON(http.StatusOK, gin.H{"seat_id": seatID})
}

func (h *BookingHandler) Confirm(c *gin.Context) {
	idStr := c.Param("id")
	eventID, err := uuid.Parse(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid event id"})
		return
	}

	idSeatStr := c.Param("seat_id")
	seatID, err := uuid.Parse(idSeatStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid seat id"})
		return
	}

	var req reserveRequest
	err = c.ShouldBindJSON(&req)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}

	err = h.svc.Confirm(c.Request.Context(), eventID, seatID, req.UserID)
	if err != nil {
		c.JSON(handlingError(err))
		return
	}

	c.JSON(http.StatusOK, gin.H{"seat_id": seatID})
}

func handlingError(err error) (int, gin.H) {
	switch {
	case errors.Is(err, domain.ErrNoSeatsAvailable):
		return http.StatusConflict, gin.H{"error": "no seats available"}
	case errors.Is(err, domain.ErrSeatNotAvailable):
		return http.StatusConflict, gin.H{"error": "seat not available"}
	case errors.Is(err, domain.ErrSeatNotFound):
		return http.StatusNotFound, gin.H{"error": "seat not found"}
	case errors.Is(err, domain.ErrHoldExpired):
		return http.StatusConflict, gin.H{"error": "hold expired"}
	case errors.Is(err, domain.ErrNotHolder):
		return http.StatusForbidden, gin.H{"error": "seat is held by another user"}
	default:
		log.Printf("internal error: %v", err)
		return http.StatusInternalServerError, gin.H{"error": "internal error"}
	}
}

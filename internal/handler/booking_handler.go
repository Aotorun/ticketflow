package handler

import (
	"errors"
	"log"
	"net/http"
	"time"

	"github.com/Aotorun/ticketflow/internal/domain"
	"github.com/Aotorun/ticketflow/internal/service"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

const holdTTL = 5 * time.Minute

type reserveRequest struct {
	UserID uuid.UUID `json:"user_id" binding:"required"`
}

type SeatResponse struct {
	SeatID    uuid.UUID         `json:"seat_id"`
	Status    domain.SeatStatus `json:"status"`
	HeldUntil *time.Time        `json:"held_until,omitempty"`
}

func toSeatResponse(seat domain.EventSeat) SeatResponse {
	return SeatResponse{
		SeatID:    seat.SeatID,
		Status:    seat.Status,
		HeldUntil: seat.HeldUntil,
	}
}

type BookingHandler struct {
	svc *service.BookingService
}

func NewBookingHandler(svc *service.BookingService) *BookingHandler {
	return &BookingHandler{svc}
}

func (h *BookingHandler) GetSeatMap(c *gin.Context) {
	eventID, ok := parseUUIDParam(c, "id")
	if !ok {
		return
	}

	seats, err := h.svc.GetSeatMap(c.Request.Context(), eventID)
	if err != nil {
		log.Print(err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}

	resp := make([]SeatResponse, 0, len(seats))
	for _, s := range seats {
		resp = append(resp, toSeatResponse(s))
	}
	c.JSON(http.StatusOK, resp)
}

func (h *BookingHandler) ReserveAny(c *gin.Context) {
	eventID, ok := parseUUIDParam(c, "id")
	if !ok {
		return
	}

	req, ok := bindReserve(c)
	if !ok {
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
	eventID, ok := parseUUIDParam(c, "id")
	if !ok {
		return
	}

	seatID, ok := parseUUIDParam(c, "seat_id")
	if !ok {
		return
	}

	req, ok := bindReserve(c)
	if !ok {
		return
	}

	err := h.svc.ReserveSpecific(c.Request.Context(), eventID, seatID, req.UserID, holdTTL)
	if err != nil {
		c.JSON(handlingError(err))
		return
	}

	c.JSON(http.StatusOK, gin.H{"seat_id": seatID})
}

func (h *BookingHandler) Confirm(c *gin.Context) {
	eventID, ok := parseUUIDParam(c, "id")
	if !ok {
		return
	}

	seatID, ok := parseUUIDParam(c, "seat_id")
	if !ok {
		return
	}

	req, ok := bindReserve(c)
	if !ok {
		return
	}

	err := h.svc.Confirm(c.Request.Context(), eventID, seatID, req.UserID)
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

func parseUUIDParam(c *gin.Context, name string) (uuid.UUID, bool) {
	paramStr := c.Param(name)
	param, err := uuid.Parse(paramStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid " + name})
		return uuid.Nil, false
	}

	return param, true
}

func bindReserve(c *gin.Context) (reserveRequest, bool) {
	var req reserveRequest
	err := c.ShouldBindJSON(&req)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return req, false
	}

	return req, true
}

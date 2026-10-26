package handlers

import (
	"crypto/sha512"
	"crypto/subtle"
	"ecommerce/payment-service/config"
	"ecommerce/payment-service/internal/adapter"
	"ecommerce/payment-service/internal/adapter/handlers/request"
	"ecommerce/payment-service/internal/adapter/handlers/response"
	"ecommerce/payment-service/internal/core/domain/entity"
	"ecommerce/payment-service/internal/core/service"
	"ecommerce/payment-service/utils/conv"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/labstack/gommon/log"
)

type PaymentHandlerInterface interface {
	Create(c echo.Context) error
	Confirm(c echo.Context) error
	MidtranswebHookHandler(c echo.Context) error
	GetAllAdmin(c echo.Context) error
	GetAllCustomer(c echo.Context) error
	GetDetail(c echo.Context) error
}

func (p *paymentHandler) Confirm(c echo.Context) error {
	user := c.Get("user").(string)
	if user == "" {
		return c.JSON(http.StatusUnauthorized, response.ResponseDefault("data token not found", nil))
	}

	orderID, err := conv.StringToInt64(c.Param("orderID"))
	if err != nil || orderID <= 0 {
		return c.JSON(http.StatusBadRequest, response.ResponseDefault("invalid order_id", nil))
	}

	status, err := p.paymentService.ConfirmPayment(c.Request().Context(), orderID, user)
	if err != nil {
		log.Errorf("[PaymentHandler] Confirm: %v", err)
		return c.JSON(http.StatusInternalServerError, response.ResponseDefault(err.Error(), nil))
	}

	return c.JSON(http.StatusOK, response.ResponseDefault("success", map[string]string{
		"payment_status": status,
	}))
}

type paymentHandler struct {
	paymentService service.PaymentServiceInterface
	cfg            *config.Config
}

// Create implements [PaymentHandlerInterface].
func (p *paymentHandler) Create(c echo.Context) error {
	var (
		ctx = c.Request().Context()
		req = request.PaymentRequest{}
	)

	user := c.Get("user").(string)
	if user == "" {
		log.Errorf("[PaymentHandler-1] Create: %s", "data token not found")
		return c.JSON(http.StatusUnauthorized, response.ResponseDefault("data token not found", nil))
	}

	if err := c.Bind(&req); err != nil {
		log.Errorf("[PaymentHandler-2] Create: %v", err)
		return c.JSON(http.StatusBadRequest, response.ResponseDefault(err.Error(), nil))
	}

	if err := c.Validate(&req); err != nil {
		log.Errorf("[PaymentHandler-3] Create: %v", err)
		return c.JSON(http.StatusUnprocessableEntity, response.ResponseDefault(err.Error(), nil))
	}

	paymentEntity := entity.PaymentEntity{
		OrderID:       req.OrderID,
		PaymentMethod: entity.PaymentMethod(req.PaymentMethod),
		GrossAmount:   float64(req.GrossAmount),
		UserID:        req.UserID,
		Remarks:       req.Remarks,
	}

	result, err := p.paymentService.ProcessPayment(ctx, paymentEntity, user)
	if err != nil {
		log.Errorf("[PaymentHandler-4] Create: %v", err)
		return c.JSON(http.StatusInternalServerError, response.ResponseDefault(err.Error(), nil))
	}

	responPayment := map[string]interface{}{
		"payment_token": result.PaymentGatewayID,
	}

	return c.JSON(http.StatusCreated, response.ResponseDefault("success", responPayment))
}

// GetAllAdmin implements [PaymentHandlerInterface].
func (p *paymentHandler) GetAllAdmin(c echo.Context) error {
	var (
		ctx   = c.Request().Context()
		resps = []response.PaymentListResponse{}
	)

	user := c.Get("user").(string)
	if user == "" {
		log.Errorf("[PaymentHandler-1] GetAll: %s", "data token not found")
		return c.JSON(http.StatusNotFound, response.ResponseDefault("data token not found", nil))
	}

	userID := 0
	search := c.QueryParam("search")
	var page int64 = 1
	if pageStr := c.QueryParam("page"); pageStr != "" {
		page, _ = conv.StringToInt64(pageStr)
		if page <= 0 {
			page = 1
		}
	}

	var perPage int64 = 10
	if perPageStr := c.QueryParam("perPage"); perPageStr != "" {
		perPage, _ = conv.StringToInt64(perPageStr)
		if perPage <= 0 {
			perPage = 10
		}
	}

	status := ""
	if statusStr := c.QueryParam("status"); statusStr != "" {
		status = statusStr
	}

	orderBy := "created_at"
	if orderByStr := c.QueryParam("orderBy"); orderByStr != "" {
		orderBy = orderByStr
	}

	orderType := "desc"
	if orderTypeStr := c.QueryParam("orderType"); orderTypeStr != "" {
		orderType = orderTypeStr
	}

	reqEntity := entity.PaymentQueryStringRequest{
		Search:    search,
		Status:    status,
		Page:      page,
		Limit:     perPage,
		OrderBy:   orderBy,
		OrderType: orderType,
		UserID:    int64(userID),
	}

	results, count, total, err := p.paymentService.GetAll(ctx, reqEntity, user)
	if err != nil {
		log.Errorf("[PaymentHandler-3] GetAll: %v", err)
		return c.JSON(http.StatusInternalServerError, response.ResponseDefault(err.Error(), nil))
	}

	for _, val := range results {
		resps = append(resps, response.PaymentListResponse{
			ID:            uint64(val.ID),
			OrderCode:     val.OrderCode,
			PaymentStatus: val.PaymentStatus,
			PaymentMethod: string(val.PaymentMethod),
			GrossAmount:   val.GrossAmount,
			ShippingType:  val.OrderShippingType,
		})
	}

	return c.JSON(http.StatusOK, response.ResponseSuccessWithPagination("success", resps, page, count, total, perPage))
}

// GetAllCustomer implements [PaymentHandlerInterface].
func (p *paymentHandler) GetAllCustomer(c echo.Context) error {
	var (
		ctx         = c.Request().Context()
		resps       = []response.PaymentListResponse{}
		jwtUserData = entity.JwtUserData{}
	)

	user := c.Get("user").(string)
	if user == "" {
		log.Errorf("[PaymentHandler-1] GetAll: %s", "data token not found")
		return c.JSON(http.StatusNotFound, response.ResponseDefault("data token not found", nil))
	}

	err := json.Unmarshal([]byte(user), &jwtUserData)
	if err != nil {
		log.Errorf("[PaymentHandler-2] GetAllCustomer: %v", err)
		return c.JSON(http.StatusBadRequest, response.ResponseDefault(err.Error(), nil))
	}

	userID := jwtUserData.UserID
	search := c.QueryParam("search")
	var page int64 = 1
	if pageStr := c.QueryParam("page"); pageStr != "" {
		page, _ = conv.StringToInt64(pageStr)
		if page <= 0 {
			page = 1
		}
	}

	var perPage int64 = 10
	if perPageStr := c.QueryParam("perPage"); perPageStr != "" {
		perPage, _ = conv.StringToInt64(perPageStr)
		if perPage <= 0 {
			perPage = 10
		}
	}

	status := ""
	if statusStr := c.QueryParam("status"); statusStr != "" {
		status = statusStr
	}

	orderBy := "created_at"
	if orderByStr := c.QueryParam("orderBy"); orderByStr != "" {
		orderBy = orderByStr
	}

	orderType := "desc"
	if orderTypeStr := c.QueryParam("orderType"); orderTypeStr != "" {
		orderType = orderTypeStr
	}

	reqEntity := entity.PaymentQueryStringRequest{
		Search:    search,
		Status:    status,
		Page:      page,
		Limit:     perPage,
		OrderBy:   orderBy,
		OrderType: orderType,
		UserID:    int64(userID),
	}

	results, count, total, err := p.paymentService.GetAll(ctx, reqEntity, user)
	if err != nil {
		log.Errorf("[PaymentHandler-3] GetAll: %v", err)
		return c.JSON(http.StatusInternalServerError, response.ResponseDefault(err.Error(), nil))
	}

	for _, val := range results {
		resps = append(resps, response.PaymentListResponse{
			ID:            uint64(val.ID),
			OrderCode:     val.OrderCode,
			PaymentStatus: val.PaymentStatus,
			PaymentMethod: string(val.PaymentMethod),
			GrossAmount:   val.GrossAmount,
			ShippingType:  val.OrderShippingType,
		})
	}

	return c.JSON(http.StatusOK, response.ResponseSuccessWithPagination("success", resps, page, count, total, perPage))
}

// GetDetail implements [PaymentHandlerInterface].
func (p *paymentHandler) GetDetail(c echo.Context) error {
	var (
		ctx   = c.Request().Context()
		resps = response.PaymentDetailResponse{}
	)

	user := c.Get("user").(string)
	if user == "" {
		log.Errorf("[PaymentHandler-1] GetAll: %s", "data token not found")
		return c.JSON(http.StatusNotFound, response.ResponseDefault("data token not found", nil))
	}

	paymentID := c.Param("id")
	if paymentID == "" {
		log.Errorf("[PaymentHandler-2] GetDetail: %s", "Payment ID is required")
		return c.JSON(http.StatusBadRequest, response.ResponseDefault("Payment ID is required", nil))
	}

	paymentIDInt, err := conv.StringToInt64(paymentID)
	if err != nil {
		log.Errorf("[PaymentHandler-3] GetDetail: %v", err)
		return c.JSON(http.StatusBadRequest, response.ResponseDefault(err.Error(), nil))
	}

	result, err := p.paymentService.GetDetail(ctx, uint(paymentIDInt), user)
	if err != nil {
		log.Errorf("[PaymentHandler-4] GetDetail: %v", err)
		if err.Error() == "404" || strings.Contains(err.Error(), "order service returned HTTP 404") {
			return c.JSON(http.StatusNotFound, response.ResponseDefault("data not found", nil))
		}
		return c.JSON(http.StatusInternalServerError, response.ResponseDefault(err.Error(), nil))
	}

	resps.ID = int64(result.ID)
	resps.OrderCode = result.OrderCode
	resps.PaymentMethod = string(result.PaymentMethod)
	resps.PaymentStatus = result.PaymentStatus
	resps.GrossAmount = result.GrossAmount
	resps.ShippingType = result.OrderShippingType
	resps.PaymentAt = result.PaymentAt
	resps.OrderAt = result.OrderAt
	resps.OrderRemarks = result.OrderRemarks
	resps.CustomerName = result.CustomerName
	resps.CustomerAddress = result.CustomerAddress

	return c.JSON(http.StatusOK, response.ResponseDefault("success", resps))
}

// MidtranswebHookHandler implements [PaymentHandlerInterface].
func (p *paymentHandler) MidtranswebHookHandler(c echo.Context) error {
	var notificationPayload struct {
		TransactionStatus string `json:"transaction_status"`
		OrderID           string `json:"order_id"`
		StatusCode        string `json:"status_code"`
		GrossAmount       string `json:"gross_amount"`
		SignatureKey      string `json:"signature_key"`
	}
	if err := c.Bind(&notificationPayload); err != nil {
		log.Errorf("[PaymentHandler-1] MidtranswebHookHandler: %v", err)
		return c.JSON(http.StatusBadRequest, response.ResponseDefault(err.Error(), nil))
	}
	if notificationPayload.TransactionStatus == "" || notificationPayload.OrderID == "" ||
		notificationPayload.StatusCode == "" || notificationPayload.GrossAmount == "" ||
		notificationPayload.SignatureKey == "" {
		return c.JSON(http.StatusBadRequest, response.ResponseDefault("invalid Midtrans notification payload", nil))
	}
	if p.cfg.Midtrans.ServerKey == "" {
		log.Errorf("[PaymentHandler] Midtrans server key is not configured")
		return c.JSON(http.StatusInternalServerError, response.ResponseDefault("Midtrans server key is not configured", nil))
	}

	signature := sha512.Sum512([]byte(notificationPayload.OrderID + notificationPayload.StatusCode + notificationPayload.GrossAmount + p.cfg.Midtrans.ServerKey))
	expectedSignature := hex.EncodeToString(signature[:])
	if subtle.ConstantTimeCompare([]byte(strings.ToLower(notificationPayload.SignatureKey)), []byte(expectedSignature)) != 1 {
		return c.JSON(http.StatusUnauthorized, response.ResponseDefault("invalid Midtrans notification signature", nil))
	}

	newStatus := mapMidtransWebhookStatus(notificationPayload.TransactionStatus)

	if err := p.paymentService.UpdateStatusByOrderCode(c.Request().Context(), notificationPayload.OrderID, newStatus); err != nil {
		log.Errorf("[PaymentHandler-3] MidtranswebHookHandler: %v", err)
		return c.JSON(http.StatusInternalServerError, response.ResponseDefault(err.Error(), nil))
	}

	return c.JSON(http.StatusOK, response.ResponseDefault("success", nil))
}

func mapMidtransWebhookStatus(transactionStatus string) string {
	switch strings.ToUpper(transactionStatus) {
	case entity.TransactionStatusCapture, entity.TransactionStatusSettlement, entity.TransactionStatusSuccess:
		return entity.PaymentStatusSuccess
	case entity.TransactionStatusExpire:
		return entity.PaymentStatusExpired
	case entity.TransactionStatusDeny, entity.TransactionStatusCancel:
		return entity.PaymentStatusFailed
	case entity.TransactionStatusPending:
		return entity.PaymentStatusPending
	default:
		return entity.PaymentStatusUnknown
	}
}

func NewPaymentHandler(paymentService service.PaymentServiceInterface, e *echo.Echo, cfg *config.Config) PaymentHandlerInterface {
	paymentHandler := &paymentHandler{
		paymentService: paymentService,
		cfg:            cfg,
	}
	e.Use(middleware.Recover())
	mid := adapter.NewMiddlewareAdapter(cfg)
	e.POST("/payments/webhook", paymentHandler.MidtranswebHookHandler)
	authGroup := e.Group("auth", mid.CheckToken())
	authGroup.GET("/payments", paymentHandler.GetAllCustomer)
	authGroup.GET("/payments/:id", paymentHandler.GetDetail)
	authGroup.POST("/payments", paymentHandler.Create)
	authGroup.POST("/payments/:orderID/confirm", paymentHandler.Confirm)

	adminGroup := e.Group("/admin", mid.CheckToken())
	adminGroup.GET("/payments", paymentHandler.GetAllAdmin)
	adminGroup.GET("/payments/:id", paymentHandler.GetDetail)

	return paymentHandler

}

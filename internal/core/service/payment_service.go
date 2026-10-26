package service

import (
	"context"
	"ecommerce/payment-service/config"
	httpclient "ecommerce/payment-service/internal/adapter/http_client"
	"ecommerce/payment-service/internal/adapter/message"
	"ecommerce/payment-service/internal/adapter/repository"
	"ecommerce/payment-service/internal/core/domain/entity"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/labstack/gommon/log"
)

type PaymentServiceInterface interface {
	ProcessPayment(ctx context.Context, payment entity.PaymentEntity, accessToken string) (*entity.PaymentEntity, error)
	UpdateStatusByOrderCode(ctx context.Context, orderCode, status string) error
	ConfirmPayment(ctx context.Context, orderID int64, accessToken string) (string, error)
	GetAll(ctx context.Context, req entity.PaymentQueryStringRequest, accessToken string) ([]entity.PaymentEntity, int64, int64, error)
	GetDetail(ctx context.Context, paymentID uint, accessToken string) (*entity.PaymentEntity, error)
	httpClientOrderService(orderId int64, accessToken string, isAdmin bool) (*entity.OrderDetailHttpResponse, error)
	httpClientUserService(accessToken string, userID int64, isAdmin bool) (*entity.ProfileHttpResponse, error)
	httpClientOrderByCodeService(orderCode string, accessToken string) (*entity.OrderDetailHttpResponse, error)
	httpClientPublicOrderIDByCodeService(orderCode string) (int64, error)
}

type paymentService struct {
	repo                repository.PaymentRepositoryInterface
	httpClientToService httpclient.HttpClientToService
	midtrans            httpclient.MidtransClientInterface
	cfg                 *config.Config
	publisherRabbitMQ   message.PublishRabbitMQInterface
}

func (p *paymentService) serviceRequestHeaders(accessToken string) map[string]string {
	headers := map[string]string{
		"Accept":        "application/json",
		"Content-Type":  "application/json",
		"X-API-Gateway": "true",
	}
	if accessToken != "" {
		headers["Authorization"] = "Bearer " + accessToken
	}
	if p.cfg.App.GatewaySecretKey != "" {
		headers["X-Gateway-Secret"] = p.cfg.App.GatewaySecretKey
	}
	return headers
}

// HttpClientOrderByCodeService implements [PaymentServiceInterface].
func (p *paymentService) httpClientOrderByCodeService(orderCode string, accessToken string) (*entity.OrderDetailHttpResponse, error) {
	baseUrlOrder := fmt.Sprintf("%s/%s", p.cfg.App.OrderServiceUrl, "auth/orders/"+orderCode+"/code")
	dataOrder, err := p.httpClientToService.CallURL("GET", baseUrlOrder, p.serviceRequestHeaders(accessToken), nil)
	if err != nil {
		log.Errorf("[PaymentService] httpClientOrderByCodeService-1: %v", err)
		return nil, err
	}

	defer dataOrder.Body.Close()

	body, err := io.ReadAll(dataOrder.Body)
	if err != nil {
		log.Errorf("[PaymentService] httpClientOrderByCodeService-2: %v", err)
		return nil, err
	}
	if dataOrder.StatusCode < http.StatusOK || dataOrder.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("order service returned HTTP %d: %s", dataOrder.StatusCode, strings.TrimSpace(string(body)))
	}

	var orderDetail entity.OrderHttpClientResponse
	err = json.Unmarshal([]byte(body), &orderDetail)
	if err != nil {
		log.Errorf("[PaymentService] httpClientOrderByCodeService-3: %v", err)
		return nil, err
	}
	if orderDetail.Data.OrderCode == "" {
		return nil, errors.New("order service response is missing data.order_code")
	}

	return &orderDetail.Data, nil
}

// HttpClientOrderService implements [PaymentServiceInterface].
func (p *paymentService) httpClientOrderService(orderId int64, accessToken string, isAdmin bool) (*entity.OrderDetailHttpResponse, error) {
	orderPath := "auth/orders/"
	if isAdmin {
		// Admin payment pages must use the admin order endpoint. The customer
		// endpoint intentionally applies buyer ownership checks.
		orderPath = "admin/orders/"
	}
	baseUrlOrder := fmt.Sprintf("%s/%s", p.cfg.App.OrderServiceUrl, orderPath+strconv.FormatInt(orderId, 10))
	dataOrder, err := p.httpClientToService.CallURL("GET", baseUrlOrder, p.serviceRequestHeaders(accessToken), nil)
	if err != nil {
		log.Errorf("[PaymentService] httpClientOrderService-1: %v", err)
		return nil, err
	}

	defer dataOrder.Body.Close()

	body, err := io.ReadAll(dataOrder.Body)
	if err != nil {
		log.Errorf("[PaymentService] httpClientOrderService-2: %v", err)
		return nil, err
	}
	if dataOrder.StatusCode < http.StatusOK || dataOrder.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("order service returned HTTP %d: %s", dataOrder.StatusCode, strings.TrimSpace(string(body)))
	}

	var orderDetail entity.OrderHttpClientResponse
	err = json.Unmarshal([]byte(body), &orderDetail)
	if err != nil {
		log.Errorf("[PaymentService] httpClientOrderService-3: %v", err)
		return nil, err
	}
	if orderDetail.Data.OrderCode == "" {
		return nil, errors.New("order service response is missing data.order_code")
	}

	return &orderDetail.Data, nil
}

// HttpClientPublicOrderIDByCodeService implements [PaymentServiceInterface].
func (p *paymentService) httpClientPublicOrderIDByCodeService(orderCode string) (int64, error) {
	baseUrlOrder := fmt.Sprintf("%s/%s", p.cfg.App.OrderServiceUrl, "public/orders/"+orderCode+"/code")
	dataOrder, err := p.httpClientToService.CallURL("GET", baseUrlOrder, p.serviceRequestHeaders(""), nil)
	if err != nil {
		log.Errorf("[PaymentService] httpClientOrderByCodeService-1: %v", err)
		return 0, err
	}

	defer dataOrder.Body.Close()

	if dataOrder.StatusCode != 200 {
		log.Errorf("[PaymentService] httpClientOrderByCodeService-3: %v", err)
		return 0, errors.New("Order not found")
	}

	body, err := io.ReadAll(dataOrder.Body)
	if err != nil {
		log.Errorf("[PaymentService] httpClientOrderByCodeService-2: %v", err)
		return 0, err
	}

	var orderDetail entity.GetOrderIDByCodeResponse
	err = json.Unmarshal([]byte(body), &orderDetail)
	if err != nil {
		log.Errorf("[PaymentService] httpClientOrderByCodeService-4: %v", err)
		return 0, err
	}

	return int64(orderDetail.Data.OrderID), nil
}

// HttpClientUserService implements [PaymentServiceInterface].
func (p *paymentService) httpClientUserService(accessToken string, userID int64, isAdmin bool) (*entity.ProfileHttpResponse, error) {
	baseUrlUser := fmt.Sprintf("%s/%s", p.cfg.App.UserServiceUrl, "auth/profile")
	if isAdmin {
		baseUrlUser = fmt.Sprintf("%s/%s", p.cfg.App.UserServiceUrl, "admin/customers/"+strconv.FormatInt(userID, 10))
	}
	dataUser, err := p.httpClientToService.CallURL("GET", baseUrlUser, p.serviceRequestHeaders(accessToken), nil)
	if err != nil {
		log.Errorf("[PaymentService] httpClientUserService-1: %v", err)
		return nil, err
	}

	defer dataUser.Body.Close()

	body, err := io.ReadAll(dataUser.Body)
	if err != nil {
		log.Errorf("[PaymentService] httpClientUserService-2: %v", err)
		return nil, err
	}

	var userResponse entity.UserHttpClientResponse
	err = json.Unmarshal([]byte(body), &userResponse)
	if err != nil {
		log.Errorf("[PaymentService] httpClientUserService-3: %v", err)
		return nil, err
	}

	return &userResponse.Data, nil
}

// GetAll implements [PaymentServiceInterface].
func (p *paymentService) GetAll(ctx context.Context, req entity.PaymentQueryStringRequest, accessToken string) ([]entity.PaymentEntity, int64, int64, error) {
	results, count, total, err := p.repo.GetAll(ctx, req)
	if err != nil {
		log.Errorf("[PaymentService] GetAll-1: %v", err)
		return nil, 0, 0, err
	}

	var token map[string]interface{}
	err = json.Unmarshal([]byte(accessToken), &token)
	if err != nil {
		log.Errorf("[PaymentService] GetAll-2: %v", err)
		return nil, 0, 0, err
	}
	roleName, _ := token["role_name"].(string)
	isAdmin := roleName == "Admin" || roleName == "Super Admin"
	serviceToken, ok := token["token"].(string)
	if !ok || serviceToken == "" {
		return nil, 0, 0, errors.New("invalid user token")
	}
	validResults := make([]entity.PaymentEntity, 0, len(results))
	for _, val := range results {
		orderDetail, err := p.httpClientOrderService(int64(val.OrderID), serviceToken, isAdmin)
		if err != nil {
			// Historical payment rows can remain after their order is removed.
			// Do not make the complete payment list unavailable because of one
			// orphan row; valid rows are still returned to the caller.
			if strings.Contains(err.Error(), "order service returned HTTP 404") {
				log.Warnf("[PaymentService] payment %d references missing order %d", val.ID, val.OrderID)
				continue
			}
			log.Errorf("[PaymentService] GetAll-3: %v", err)
			return nil, 0, 0, err
		}
		if status, syncErr := p.syncPendingPaymentStatus(ctx, &val, orderDetail.OrderCode); syncErr != nil {
			log.Warnf("[PaymentService] unable to sync payment %d status: %v", val.ID, syncErr)
		} else if status != "" {
			val.PaymentStatus = status
		}
		val.OrderCode = orderDetail.OrderCode
		val.OrderShippingType = orderDetail.ShippingType
		validResults = append(validResults, val)
	}

	return validResults, count, total, nil
}

// GetDetail implements [PaymentServiceInterface].
func (p *paymentService) GetDetail(ctx context.Context, paymentID uint, accessToken string) (*entity.PaymentEntity, error) {
	result, err := p.repo.GetDetail(ctx, paymentID)
	if err != nil {
		log.Errorf("[PaymentService] GetDetail-1: %v", err)
		return nil, err
	}

	var token map[string]interface{}
	err = json.Unmarshal([]byte(accessToken), &token)
	if err != nil {
		log.Errorf("[PaymentService] GetDetail-2: %v", err)
		return nil, err
	}
	roleName, _ := token["role_name"].(string)
	isAdmin := roleName == "Admin" || roleName == "Super Admin"

	serviceToken, ok := token["token"].(string)
	if !ok || serviceToken == "" {
		return nil, errors.New("invalid user token")
	}
	orderDetail, err := p.httpClientOrderService(int64(result.OrderID), serviceToken, isAdmin)
	if err != nil {
		log.Errorf("[PaymentService] GetDetail-3: %v", err)
		return nil, err
	}
	if status, syncErr := p.syncPendingPaymentStatus(ctx, result, orderDetail.OrderCode); syncErr != nil {
		log.Warnf("[PaymentService] unable to sync payment %d status: %v", result.ID, syncErr)
	} else if status != "" {
		result.PaymentStatus = status
	}

	// Use the buyer from the order as the source of truth. This prevents a
	// stale payment user_id from showing the wrong customer in the detail view.
	userID := orderDetail.Customer.CustomerID
	if userID <= 0 {
		userID = int64(result.UserID)
	}

	userDetail, err := p.httpClientUserService(serviceToken, userID, isAdmin)
	if err != nil {
		log.Errorf("[PaymentService] GetDetail-4: %v", err)
		return nil, err
	}

	result.CustomerName = userDetail.Name
	result.CustomerEmail = userDetail.Email
	result.CustomerAddress = userDetail.Address

	result.OrderCode = orderDetail.OrderCode
	result.OrderShippingType = orderDetail.ShippingType
	result.OrderAt = orderDetail.OrderDatetime
	result.OrderRemarks = orderDetail.Remarks

	return result, nil
}

// ProcessPayment implements [PaymentServiceInterface].
func (p *paymentService) ProcessPayment(ctx context.Context, payment entity.PaymentEntity, accessToken string) (*entity.PaymentEntity, error) {
	_, err := p.repo.GetByOrderID(ctx, uint(payment.OrderID))
	if err == nil {
		log.Infof("[PaymentService] ProcessPayment-1: Payment already exists")
		return nil, errors.New("Payment already exists")
	}

	switch strings.ToUpper(string(payment.PaymentMethod)) {
	case string(entity.PaymentMethodCod):
		payment.PaymentMethod = entity.PaymentMethodCod
		payment.PaymentStatus = entity.PaymentStatusSuccess
		var token map[string]interface{}
		if err := json.Unmarshal([]byte(accessToken), &token); err != nil {
			return nil, err
		}
		serviceToken, ok := token["token"].(string)
		if !ok || serviceToken == "" {
			return nil, errors.New("invalid user token")
		}
		orderDetail, err := p.httpClientOrderService(int64(payment.OrderID), serviceToken, false)
		if err != nil {
			return nil, err
		}

		if err := p.repo.CreatePayment(ctx, payment); err != nil {
			log.Errorf("[PaymentService] ProcessPayment-2: %v", err)
			return nil, err
		}
		if err := p.notifyOrderPaymentSucceeded(ctx, orderDetail.OrderCode); err != nil {
			// The payment is already persisted. Keep its response successful and
			// log callback failures so operations can reconcile the order.
			log.Errorf("[PaymentService] COD order-success callback failed for %s: %v", orderDetail.OrderCode, err)
		}

		if err := p.publisherRabbitMQ.PublishPaymentSuccess(payment); err != nil {
			log.Errorf("[PaymentService] ProcessPayment-3: %v", err)
		}

		return &payment, nil
	case string(entity.PaymentMethodMidtrans):
		payment.PaymentMethod = entity.PaymentMethodMidtrans
		var token map[string]interface{}
		err := json.Unmarshal([]byte(accessToken), &token)
		if err != nil {
			log.Errorf("[PaymentService] ProcessPayment-4: %v", err)
			return nil, err
		}

		isAdmin := false
		if token["role_name"].(string) == "Super Admin" {
			isAdmin = true
		}

		userResponse, err := p.httpClientUserService(token["token"].(string), int64(payment.UserID), isAdmin)
		if err != nil {
			log.Errorf("[PaymentService] ProcessPayment-5: %v", err)
			return nil, err
		}

		orderDetail, err := p.httpClientOrderService(int64(payment.OrderID), token["token"].(string), false)
		if err != nil {
			log.Errorf("[PaymentService] ProcessPayment-6: %v", err)
			return nil, err
		}

		transactionID, err := p.midtrans.CreateTransaction(orderDetail.OrderCode, int64(payment.GrossAmount), userResponse.Name, userResponse.Email)
		if err != nil {
			log.Errorf("[PaymentService] ProcessPayment-7: %v", err)
			return nil, err
		}
		payment.PaymentStatus = entity.PaymentStatusPending
		payment.PaymentGatewayID = transactionID

		if err := p.repo.CreatePayment(ctx, payment); err != nil {
			log.Errorf("[PaymentService] ProcessPayment-8: %v", err)
			return nil, err
		}

		if err := p.publisherRabbitMQ.PublishPaymentSuccess(payment); err != nil {
			log.Errorf("[PaymentService] ProcessPayment-9: %v", err)
		}

		return &payment, nil
	default:
		return nil, errors.New("Invalid payment method")
	}
}

// ConfirmPayment is the browser-safe bridge to the internal order callback.
// Nuxt never calls /internal/orders/payment-success directly; this method
// verifies the Midtrans state first and then performs the trusted callback.
func (p *paymentService) ConfirmPayment(ctx context.Context, orderID int64, accessToken string) (string, error) {
	var token map[string]interface{}
	if err := json.Unmarshal([]byte(accessToken), &token); err != nil {
		return "", err
	}
	serviceToken, ok := token["token"].(string)
	if !ok || serviceToken == "" {
		return "", errors.New("invalid user token")
	}

	payment, err := p.repo.GetByOrderID(ctx, uint(orderID))
	if err != nil {
		return "", err
	}
	if payment.PaymentStatus == entity.PaymentStatusSuccess {
		return entity.PaymentStatusSuccess, nil
	}

	orderDetail, err := p.httpClientOrderService(orderID, serviceToken, false)
	if err != nil {
		return "", err
	}

	transactionStatus, err := p.midtrans.CheckTransaction(orderDetail.OrderCode)
	if err != nil {
		return "", err
	}
	status := mapMidtransStatus(transactionStatus)
	if status == "" {
		return "", fmt.Errorf("unsupported Midtrans transaction status: %s", transactionStatus)
	}
	if status == entity.PaymentStatusPending {
		return status, nil
	}

	if err := p.UpdateStatusByOrderCode(ctx, orderDetail.OrderCode, status); err != nil {
		return "", err
	}
	return status, nil
}

// UpdateStatusByOrderCode implements [PaymentServiceInterface].
func (p *paymentService) UpdateStatusByOrderCode(ctx context.Context, orderCode string, status string) error {
	orderDetailID, err := p.httpClientPublicOrderIDByCodeService(orderCode)
	if err != nil {
		log.Errorf("[PaymentService] UpdateStatusByOrderCode-1: %v", err)
		return err
	}

	if err = p.repo.UpdateStatusByOrderCode(ctx, uint(orderDetailID), status); err != nil {
		log.Errorf("[PaymentService] UpdateStatusByOrderCode-2: %v", err)
		return err
	}
	if status == entity.PaymentStatusSuccess || status == entity.PaymentStatusExpired {
		body, marshalErr := json.Marshal(map[string]string{"order_code": orderCode})
		if marshalErr != nil {
			return marshalErr
		}
		callbackPath := "payment-success"
		if status == entity.PaymentStatusExpired {
			callbackPath = "payment-expired"
		}
		url := fmt.Sprintf("%s/internal/orders/%s", p.cfg.App.OrderServiceUrl, callbackPath)
		resp, requestErr := p.httpClientToService.CallURL("POST", url, p.serviceRequestHeaders(""), body)
		if requestErr != nil {
			return requestErr
		}
		defer resp.Body.Close()
		responseBody, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			return readErr
		}
		if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
			return fmt.Errorf("order service payment callback returned HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(responseBody)))
		}
	}

	return nil
}

func (p *paymentService) syncPendingPaymentStatus(ctx context.Context, payment *entity.PaymentEntity, orderCode string) (string, error) {
	if payment.PaymentMethod != entity.PaymentMethodMidtrans || payment.PaymentStatus != entity.PaymentStatusPending {
		return payment.PaymentStatus, nil
	}

	transactionStatus, err := p.midtrans.CheckTransaction(orderCode)
	if err != nil {
		return payment.PaymentStatus, err
	}

	status := mapMidtransStatus(transactionStatus)
	if status == "" || status == payment.PaymentStatus {
		return payment.PaymentStatus, nil
	}

	if err := p.UpdateStatusByOrderCode(ctx, orderCode, status); err != nil {
		return payment.PaymentStatus, err
	}

	return status, nil
}

func mapMidtransStatus(transactionStatus string) string {
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
		return ""
	}
}

func (p *paymentService) notifyOrderPaymentSucceeded(ctx context.Context, orderCode string) error {
	body, err := json.Marshal(map[string]string{"order_code": orderCode})
	if err != nil {
		return err
	}
	url := fmt.Sprintf("%s/internal/orders/payment-success", p.cfg.App.OrderServiceUrl)
	resp, err := p.httpClientToService.CallURL("POST", url, p.serviceRequestHeaders(""), body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("order service COD callback returned HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(responseBody)))
	}
	return nil
}

func NewPaymentService(repo repository.PaymentRepositoryInterface, cfg *config.Config, httpClientToService httpclient.HttpClientToService, midtrans httpclient.MidtransClientInterface, publisherRabbitMQ message.PublishRabbitMQInterface) PaymentServiceInterface {
	return &paymentService{
		repo:                repo,
		httpClientToService: httpClientToService,
		midtrans:            midtrans,
		cfg:                 cfg,
		publisherRabbitMQ:   publisherRabbitMQ,
	}
}

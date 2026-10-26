package httpclient

import (
	"ecommerce/payment-service/config"

	"github.com/labstack/gommon/log"
	"github.com/midtrans/midtrans-go"
	"github.com/midtrans/midtrans-go/coreapi"
	"github.com/midtrans/midtrans-go/snap"
)

type MidtransClientInterface interface {
	CreateTransaction(orderID string, amount int64, customerName, customerEmail string) (string, error)
	CheckTransaction(orderID string) (string, error)
}

type midtransClient struct {
	cfg *config.Config
}

// CreateTransaction implements MidtransClientInterface.
func (m *midtransClient) CreateTransaction(orderID string, amount int64, customerName string, customerEmail string) (string, error) {
	midtrans.ServerKey = m.cfg.Midtrans.ServerKey
	midtrans.Environment = midtrans.EnvironmentType(m.cfg.Midtrans.Environment)

	snapReq := &snap.Request{
		TransactionDetails: midtrans.TransactionDetails{
			OrderID:  orderID,
			GrossAmt: amount,
		},
		CustomerDetail: &midtrans.CustomerDetails{
			FName: customerName,
			Email: customerEmail,
		},
		Expiry: &snap.ExpiryDetails{
			Unit:     "hour",
			Duration: 1,
		},
	}

	snapRes, err := snap.CreateTransaction(snapReq)
	if err != nil {
		log.Errorf("[MidtransClient-1] Failed to create transaction: %v", err)
		return "", err
	}

	return snapRes.Token, nil
}

// CheckTransaction reads the authoritative transaction state from Midtrans.
// This is a fallback for local development when Midtrans cannot reach a
// localhost webhook URL.
func (m *midtransClient) CheckTransaction(orderID string) (string, error) {
	var client coreapi.Client
	client.New(m.cfg.Midtrans.ServerKey, midtrans.EnvironmentType(m.cfg.Midtrans.Environment))

	result, midtransErr := client.CheckTransaction(orderID)
	if midtransErr != nil {
		log.Errorf("[MidtransClient-2] Failed to check transaction: %v", midtransErr)
		return "", midtransErr
	}

	return result.TransactionStatus, nil
}

func NewMidtransClient(cfg *config.Config) MidtransClientInterface {
	return &midtransClient{cfg: cfg}
}

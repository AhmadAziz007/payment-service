package app

import (
	"context"
	"ecommerce/payment-service/config"
	"ecommerce/payment-service/utils/validator"
	"os"
	"os/signal"

	"syscall"
	"time"

	"github.com/go-playground/validator/v10/translations/en"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/labstack/gommon/log"

	"ecommerce/payment-service/internal/adapter/handlers"
	httpclient "ecommerce/payment-service/internal/adapter/http_client"
	"ecommerce/payment-service/internal/adapter/message"
	"ecommerce/payment-service/internal/adapter/repository"
	"ecommerce/payment-service/internal/core/service"
	middlewareGateway "ecommerce/payment-service/internal/middleware"
)

func RunServer() {
	cfg := config.NewConfig()
	db, err := cfg.ConnectionPostgres()
	if err != nil {
		log.Fatalf("[RunServer-1] %v", err)
		return
	}

	paymentRepo := repository.NewPaymentRepository(db.DB)

	httpClient := httpclient.NewHttpClient(cfg)
	midtrans := httpclient.NewMidtransClient(cfg)

	publisherRabbitMQ := message.NewPublisherRabbitMQ(cfg)

	paymentService := service.NewPaymentService(paymentRepo, cfg, httpClient, midtrans, publisherRabbitMQ)

	e := echo.New()
	e.Use(middleware.CORS())
	e.Use(middlewareGateway.GatewayValidationMiddleware())

	customValidator := validator.NewValidator()
	en.RegisterDefaultTranslations(customValidator.Validator, customValidator.Translator)
	e.Validator = customValidator

	e.GET("/api/check", func(c echo.Context) error {
		return c.String(200, "OK")
	})

	handlers.NewPaymentHandler(paymentService, e, cfg)

	go func() {
		if cfg.App.AppPort == "" {
			cfg.App.AppPort = os.Getenv("APP_PORT")
		}

		err = e.Start(":" + cfg.App.AppPort)
		if err != nil {
			log.Fatalf("[RunServer-2] %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt)
	signal.Notify(quit, syscall.SIGTERM)

	<-quit

	log.Print("[RunServer-3] Shutting down server of 5 second...")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	e.Shutdown(ctx)
}

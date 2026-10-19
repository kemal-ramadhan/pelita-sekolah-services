// Package rabbitmq queues submitted attempts for asynchronous scoring.
package rabbitmq

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"edupilot-assesment/internal/config"
	"edupilot-assesment/internal/service"

	"github.com/google/uuid"
	"github.com/streadway/amqp"
	"go.uber.org/zap"
)

// SubmissionMessage carries only the attempt id; answers are read from the DB,
// so a redelivered message can never duplicate responses.
type SubmissionMessage struct {
	AttemptID uuid.UUID `json:"attempt_id"`
}

var publishMu sync.Mutex

// Enqueue publishes the attempt for scoring; without RabbitMQ (or when the
// publish fails) it is scored in-process in the background.
func Enqueue(attemptID uuid.UUID) {
	if err := publish(attemptID); err != nil {
		config.Logger.Warn("rabbitmq publish failed, scoring in-process", zap.Error(err), zap.String("attempt_id", attemptID.String()))
		go processWithRetry(context.Background(), attemptID)
	}
}

func publish(attemptID uuid.UUID) error {
	if config.RabbitCh == nil {
		return errors.New("rabbitmq channel unavailable")
	}
	body, _ := json.Marshal(SubmissionMessage{AttemptID: attemptID})
	publishMu.Lock()
	defer publishMu.Unlock()
	return config.RabbitCh.Publish("", config.QueueName, false, false, amqp.Publishing{
		ContentType: "application/json", DeliveryMode: amqp.Persistent, Body: body,
	})
}

// processWithRetry is the in-process path (no broker): bounded retries.
func processWithRetry(ctx context.Context, id uuid.UUID) {
	for {
		err := service.ProcessAttempt(ctx, id)
		if err == nil || errors.Is(err, service.ErrSkip) {
			return
		}
		if !service.RecordFailure(id, err) {
			return
		}
		time.Sleep(2 * time.Second)
	}
}

// StartWorkerPool consumes the queue with numWorkers goroutines and starts the
// sweeper that re-enqueues stale attempts (e.g. queued while the broker was down).
func StartWorkerPool(ctx context.Context, numWorkers int) {
	go sweeper(ctx)
	if config.RabbitCh == nil {
		config.Logger.Warn("RabbitMQ worker pool not started: channel unavailable (in-process scoring)")
		return
	}
	_ = config.RabbitCh.Qos(numWorkers*2, 0, false)
	msgs, err := config.RabbitCh.Consume(config.QueueName, "assessment_worker", false, false, false, false, nil)
	if err != nil {
		config.Logger.Error("failed to register RabbitMQ consumer", zap.Error(err))
		return
	}
	for i := 0; i < numWorkers; i++ {
		go func() {
			for {
				select {
				case <-ctx.Done():
					return
				case d, ok := <-msgs:
					if !ok {
						return
					}
					handle(ctx, d)
				}
			}
		}()
	}
	config.Logger.Info("assessment workers started", zap.Int("workers", numWorkers), zap.String("queue", config.QueueName))
}

func handle(ctx context.Context, d amqp.Delivery) {
	var msg SubmissionMessage
	if err := json.Unmarshal(d.Body, &msg); err != nil || msg.AttemptID == uuid.Nil {
		config.Logger.Warn("dropping malformed submission message")
		_ = d.Ack(false)
		return
	}
	err := service.ProcessAttempt(ctx, msg.AttemptID)
	switch {
	case err == nil, errors.Is(err, service.ErrSkip):
		_ = d.Ack(false)
	case service.RecordFailure(msg.AttemptID, err):
		time.Sleep(2 * time.Second)
		_ = d.Nack(false, true) // bounded by MaxProcessRetries
	default:
		_ = d.Ack(false) // attempt marked failed
	}
}

func sweeper(ctx context.Context) {
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			for _, id := range service.StaleAttempts(50) {
				Enqueue(id)
			}
			service.RetryAlertNotifications(ctx)
		}
	}
}

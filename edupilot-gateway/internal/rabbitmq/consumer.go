package rabbitmq

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"edupilot-gateway/internal/model"
	"edupilot-gateway/internal/service"

	"github.com/streadway/amqp"
)

// ConsumerConfig configures the audit log consumer.
type ConsumerConfig struct {
	URL      string
	Queue    string
	DLX      string // dead-letter exchange
	DLQ      string // dead-letter queue
	Prefetch int
}

// auditMessage is the wire format published by services. Only the fields below
// are accepted; id/created_at are assigned by the gateway.
type auditMessage struct {
	Service    string             `json:"service"`
	Entity     string             `json:"entity"`
	EntityID   json.RawMessage    `json:"entity_id"` // string or number
	Action     string             `json:"action"`
	BeforeData model.NullableJSON `json:"before_data"`
	AfterData  model.NullableJSON `json:"after_data"`
	CreatedBy  json.RawMessage    `json:"created_by"` // string or number
}

func rawToString(r json.RawMessage) string {
	if len(r) == 0 || string(r) == "null" {
		return ""
	}
	var s string
	if err := json.Unmarshal(r, &s); err == nil {
		return s
	}
	return strings.Trim(string(r), `"`)
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

func decode(body []byte) (*model.AuditLog, error) {
	var m auditMessage
	if err := json.Unmarshal(body, &m); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}
	if m.Entity == "" || m.Action == "" {
		return nil, errors.New("missing entity or action")
	}
	return &model.AuditLog{
		Service:    truncate(m.Service, 100),
		Entity:     truncate(m.Entity, 100),
		EntityID:   truncate(rawToString(m.EntityID), 100),
		Action:     truncate(m.Action, 50),
		BeforeData: m.BeforeData,
		AfterData:  m.AfterData,
		CreatedBy:  truncate(rawToString(m.CreatedBy), 100),
	}, nil
}

// Run consumes audit logs until ctx is cancelled, reconnecting with backoff
// whenever the connection or channel is closed.
func Run(ctx context.Context, cfg ConsumerConfig, logService service.LogService) {
	backoff := time.Second
	for {
		err := consumeOnce(ctx, cfg, logService)
		if ctx.Err() != nil {
			log.Printf("audit consumer stopped")
			return
		}
		if err == nil {
			backoff = time.Second
		}
		log.Printf("audit consumer disconnected: %v (reconnecting in %s)", err, backoff)
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		backoff *= 2
		if backoff > 30*time.Second {
			backoff = 30 * time.Second
		}
	}
}

func setupTopology(ch *amqp.Channel, cfg ConsumerConfig) error {
	if err := ch.ExchangeDeclare(cfg.DLX, "direct", true, false, false, false, nil); err != nil {
		return fmt.Errorf("declare DLX: %w", err)
	}
	if _, err := ch.QueueDeclare(cfg.DLQ, true, false, false, false, nil); err != nil {
		return fmt.Errorf("declare DLQ: %w", err)
	}
	if err := ch.QueueBind(cfg.DLQ, cfg.Queue, cfg.DLX, false, nil); err != nil {
		return fmt.Errorf("bind DLQ: %w", err)
	}
	args := amqp.Table{
		"x-dead-letter-exchange":    cfg.DLX,
		"x-dead-letter-routing-key": cfg.Queue,
	}
	if _, err := ch.QueueDeclare(cfg.Queue, true, false, false, false, args); err != nil {
		return fmt.Errorf("declare queue %s (if it already exists without DLX args, delete it once): %w", cfg.Queue, err)
	}
	if err := ch.Qos(cfg.Prefetch, 0, false); err != nil {
		return fmt.Errorf("set QoS: %w", err)
	}
	return nil
}

// consumeOnce runs a single connection lifecycle. It returns when the
// connection/channel closes or ctx is cancelled.
func consumeOnce(ctx context.Context, cfg ConsumerConfig, logService service.LogService) error {
	conn, err := amqp.DialConfig(cfg.URL, amqp.Config{Heartbeat: 10 * time.Second, Dial: amqp.DefaultDial(10 * time.Second)})
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}
	defer conn.Close()

	ch, err := conn.Channel()
	if err != nil {
		return fmt.Errorf("open channel: %w", err)
	}
	defer ch.Close()

	if err := setupTopology(ch, cfg); err != nil {
		return err
	}

	connClosed := conn.NotifyClose(make(chan *amqp.Error, 1))
	chClosed := ch.NotifyClose(make(chan *amqp.Error, 1))

	msgs, err := ch.Consume(cfg.Queue, "edupilot-gateway", false, false, false, false, nil)
	if err != nil {
		return fmt.Errorf("consume: %w", err)
	}
	log.Printf("Waiting for audit logs on queue %s", cfg.Queue)

	for {
		select {
		case <-ctx.Done():
			return nil
		case e := <-connClosed:
			return fmt.Errorf("connection closed: %v", e)
		case e := <-chClosed:
			return fmt.Errorf("channel closed: %v", e)
		case d, ok := <-msgs:
			if !ok {
				return errors.New("delivery channel closed")
			}
			handle(d, logService)
		}
	}
}

func handle(d amqp.Delivery, logService service.LogService) {
	auditLog, err := decode(d.Body)
	if err != nil {
		// Poison message: dead-letter it, never requeue.
		log.Printf("audit log rejected to DLQ: %v", err)
		if nerr := d.Nack(false, false); nerr != nil {
			log.Printf("nack failed: %v", nerr)
		}
		return
	}
	if err := logService.SaveAuditLog(auditLog); err != nil {
		// Transient DB failure: requeue once; a redelivered message that still
		// fails is dead-lettered to avoid a hot loop.
		requeue := !d.Redelivered
		log.Printf("saving audit log failed (requeue=%v): %v", requeue, err)
		if requeue {
			time.Sleep(time.Second) // brief pause before redelivery
		}
		if nerr := d.Nack(false, requeue); nerr != nil {
			log.Printf("nack failed: %v", nerr)
		}
		return
	}
	if err := d.Ack(false); err != nil {
		log.Printf("ack failed: %v", err)
	}
}

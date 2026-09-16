package kafka

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/ErenKarakus1/Trading-Engine/internal/domain"
	"github.com/ErenKarakus1/Trading-Engine/internal/matching"
	kafkago "github.com/segmentio/kafka-go"
)

func TestNewPublisherRejectsNilWriter(t *testing.T) {
	if _, err := NewPublisher(nil); err != ErrNilWriter {
		t.Fatalf("NewPublisher(nil) error = %v, want %v", err, ErrNilWriter)
	}
}

func TestPublishEventsWritesSequencedMessages(t *testing.T) {
	writer := &fakeWriter{}
	publisher, err := NewPublisher(writer)
	if err != nil {
		t.Fatalf("NewPublisher() error = %v", err)
	}

	events := []matching.Event{
		{
			Type:     domain.EventTypeOrderAccepted,
			Sequence: 7,
			Order: &matching.Order{
				ID:       "buy-1",
				Symbol:   "BTC-USD",
				Side:     domain.SideBuy,
				Type:     domain.OrderTypeLimit,
				Price:    100,
				Quantity: 5,
			},
		},
	}
	if err := publisher.PublishEvents(context.Background(), events); err != nil {
		t.Fatalf("PublishEvents() error = %v", err)
	}

	if len(writer.messages) != 1 {
		t.Fatalf("messages = %d, want 1", len(writer.messages))
	}
	message := writer.messages[0]
	if string(message.Key) != "7" {
		t.Fatalf("Key = %q, want 7", message.Key)
	}
	if len(message.Headers) != 1 || message.Headers[0].Key != "event_type" || string(message.Headers[0].Value) != string(domain.EventTypeOrderAccepted) {
		t.Fatalf("Headers = %+v", message.Headers)
	}

	var decoded matching.Event
	if err := json.Unmarshal(message.Value, &decoded); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if decoded.Sequence != 7 || decoded.Type != domain.EventTypeOrderAccepted {
		t.Fatalf("decoded event = %+v", decoded)
	}
}

func TestPublishEventsSkipsEmptyBatch(t *testing.T) {
	writer := &fakeWriter{}
	publisher, err := NewPublisher(writer)
	if err != nil {
		t.Fatalf("NewPublisher() error = %v", err)
	}

	if err := publisher.PublishEvents(context.Background(), nil); err != nil {
		t.Fatalf("PublishEvents() error = %v", err)
	}
	if writer.called {
		t.Fatal("writer called for empty batch")
	}
}

func TestPublishEventsReturnsWriterError(t *testing.T) {
	want := errors.New("write failed")
	writer := &fakeWriter{err: want}
	publisher, err := NewPublisher(writer)
	if err != nil {
		t.Fatalf("NewPublisher() error = %v", err)
	}

	err = publisher.PublishEvents(context.Background(), []matching.Event{{Type: domain.EventTypeOrderAccepted, Sequence: 1}})
	if !errors.Is(err, want) {
		t.Fatalf("PublishEvents() error = %v, want %v", err, want)
	}
}

type fakeWriter struct {
	called   bool
	closed   bool
	err      error
	messages []kafkago.Message
}

func (w *fakeWriter) WriteMessages(_ context.Context, messages ...kafkago.Message) error {
	w.called = true
	w.messages = append(w.messages, messages...)
	return w.err
}

func (w *fakeWriter) Close() error {
	w.closed = true
	return nil
}

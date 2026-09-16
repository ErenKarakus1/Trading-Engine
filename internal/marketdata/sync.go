package marketdata

import (
	"context"
	"errors"
)

var ErrUnknownMessage = errors.New("unknown market data message")

type MessageType string

const (
	MessageTypeSnapshot  MessageType = "snapshot"
	MessageTypeUpdate    MessageType = "update"
	MessageTypeHeartbeat MessageType = "heartbeat"
)

type Message struct {
	Type     MessageType
	Snapshot *Snapshot
	Update   *Update
}

type Feed interface {
	Next(context.Context) (Message, error)
	Reconnect(context.Context) error
}

type Observer interface {
	MarketDataMessage(symbol, messageType string)
	MarketDataReconnect(symbol string)
}

type Syncer struct {
	book       *Book
	feed       Feed
	observer   Observer
	reconnects int
	heartbeats int
}

func NewSyncer(book *Book, feed Feed) *Syncer {
	return &Syncer{book: book, feed: feed}
}

func NewSyncerWithObserver(book *Book, feed Feed, observer Observer) *Syncer {
	return &Syncer{book: book, feed: feed, observer: observer}
}

func (s *Syncer) Run(ctx context.Context) error {
	for {
		message, err := s.feed.Next(ctx)
		if err != nil {
			return err
		}

		if err := s.apply(ctx, message); err != nil {
			return err
		}
	}
}

func (s *Syncer) Apply(ctx context.Context, message Message) error {
	return s.apply(ctx, message)
}

func (s *Syncer) Reconnects() int {
	return s.reconnects
}

func (s *Syncer) Heartbeats() int {
	return s.heartbeats
}

func (s *Syncer) apply(ctx context.Context, message Message) error {
	s.observeMessage(message)
	switch message.Type {
	case MessageTypeSnapshot:
		if message.Snapshot == nil {
			return ErrInvalidSnapshot
		}
		return s.book.ApplySnapshot(*message.Snapshot)
	case MessageTypeUpdate:
		if message.Update == nil {
			return ErrInvalidUpdate
		}
		err := s.book.ApplyUpdate(*message.Update)
		if errors.Is(err, ErrGapDetected) {
			s.reconnects++
			if s.observer != nil && message.Update != nil {
				s.observer.MarketDataReconnect(string(message.Update.Symbol))
			}
			if reconnectErr := s.feed.Reconnect(ctx); reconnectErr != nil {
				return reconnectErr
			}
		}
		return err
	case MessageTypeHeartbeat:
		s.heartbeats++
		return nil
	default:
		return ErrUnknownMessage
	}
}

func (s *Syncer) observeMessage(message Message) {
	if s.observer == nil {
		return
	}
	switch message.Type {
	case MessageTypeSnapshot:
		if message.Snapshot != nil {
			s.observer.MarketDataMessage(string(message.Snapshot.Symbol), string(message.Type))
		}
	case MessageTypeUpdate:
		if message.Update != nil {
			s.observer.MarketDataMessage(string(message.Update.Symbol), string(message.Type))
		}
	}
}

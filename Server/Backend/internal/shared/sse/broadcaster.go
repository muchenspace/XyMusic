// Package sse provides shared polling for server-sent event topics. One poller
// is used per topic regardless of the number of connected HTTP clients.
package sse

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"xymusic/server/internal/shared/apperror"
)

type Options struct {
	RetryInterval     time.Duration
	HeartbeatInterval time.Duration
	MaxSubscribers    int
	MaxTopics         int
}

type TopicOptions struct {
	Load         func(context.Context) (any, error)
	Fingerprint  func(any) string
	Payload      func(any) any
	Event        string
	Terminal     func(any) bool
	PollInterval time.Duration
}

type Broadcaster struct {
	mu                sync.Mutex
	topics            map[string]*topic
	subscribers       int
	nextClientID      uint64
	retryInterval     time.Duration
	heartbeatInterval time.Duration
	maxSubscribers    int
	maxTopics         int
	closed            bool
}

type topic struct {
	key             string
	options         TopicOptions
	context         context.Context
	cancel          context.CancelFunc
	clients         map[uint64]chan []byte
	lastFingerprint string
	hasFingerprint  bool
	lastFrame       []byte
	lastHeartbeatAt time.Time
}

type Subscription struct {
	broadcaster *Broadcaster
	topic       *topic
	clientID    uint64
	frames      <-chan []byte
	once        sync.Once
}

func New(options Options) (*Broadcaster, error) {
	retryInterval := options.RetryInterval
	if retryInterval == 0 {
		retryInterval = 3 * time.Second
	}
	heartbeatInterval := options.HeartbeatInterval
	if heartbeatInterval == 0 {
		heartbeatInterval = 15 * time.Second
	}
	maxSubscribers := options.MaxSubscribers
	if maxSubscribers == 0 {
		maxSubscribers = 100
	}
	maxTopics := options.MaxTopics
	if maxTopics == 0 {
		maxTopics = 100
	}
	if err := validateDuration(retryInterval, 250*time.Millisecond, time.Minute, "retry interval"); err != nil {
		return nil, err
	}
	if err := validateDuration(heartbeatInterval, time.Second, time.Minute, "heartbeat interval"); err != nil {
		return nil, err
	}
	if maxSubscribers < 1 || maxSubscribers > 10_000 {
		return nil, errors.New("SSE maximum subscribers must be from 1 to 10000")
	}
	if maxTopics < 1 || maxTopics > 10_000 {
		return nil, errors.New("SSE maximum topics must be from 1 to 10000")
	}
	return &Broadcaster{
		topics: make(map[string]*topic), retryInterval: retryInterval,
		heartbeatInterval: heartbeatInterval, maxSubscribers: maxSubscribers, maxTopics: maxTopics,
	}, nil
}

func MustNew(options Options) *Broadcaster {
	broadcaster, err := New(options)
	if err != nil {
		panic(err)
	}
	return broadcaster
}

func (broadcaster *Broadcaster) Subscribe(
	requestContext context.Context,
	key string,
	options TopicOptions,
) (*Subscription, error) {
	if requestContext == nil || requestContext.Err() != nil {
		return nil, unavailable("The event stream request was cancelled; reconnect to continue")
	}
	if key == "" || options.Load == nil || options.Fingerprint == nil || options.Payload == nil {
		return nil, errors.New("SSE topic key, loader, fingerprint, and payload are required")
	}
	pollInterval := options.PollInterval
	if pollInterval == 0 {
		pollInterval = 2 * time.Second
	}
	if err := validateDuration(pollInterval, 100*time.Millisecond, time.Minute, "poll interval"); err != nil {
		return nil, err
	}
	options.PollInterval = pollInterval

	broadcaster.mu.Lock()
	if broadcaster.closed {
		broadcaster.mu.Unlock()
		return nil, unavailable("The event stream service is stopping; reconnect later")
	}
	current := broadcaster.topics[key]
	if current == nil && len(broadcaster.topics) >= broadcaster.maxTopics {
		broadcaster.mu.Unlock()
		return nil, unavailable("The event stream service is busy; retry later")
	}
	if broadcaster.subscribers >= broadcaster.maxSubscribers {
		broadcaster.mu.Unlock()
		return nil, unavailable("The event stream subscriber limit was reached; retry later")
	}
	created := false
	if current == nil {
		topicContext, cancel := context.WithCancel(context.Background())
		current = &topic{
			key: key, options: options, context: topicContext, cancel: cancel,
			clients: make(map[uint64]chan []byte), lastHeartbeatAt: time.Now(),
		}
		broadcaster.topics[key] = current
		created = true
	}
	broadcaster.nextClientID++
	clientID := broadcaster.nextClientID
	frames := make(chan []byte, 16)
	current.clients[clientID] = frames
	broadcaster.subscribers++
	if len(current.lastFrame) > 0 {
		frames <- cloneFrame(current.lastFrame)
	}
	broadcaster.mu.Unlock()

	if created {
		go broadcaster.run(current)
	}
	return &Subscription{
		broadcaster: broadcaster, topic: current, clientID: clientID, frames: frames,
	}, nil
}

func (subscription *Subscription) Frames() <-chan []byte {
	if subscription == nil {
		return nil
	}
	return subscription.frames
}

func (subscription *Subscription) Close() {
	if subscription == nil || subscription.broadcaster == nil {
		return
	}
	subscription.once.Do(func() {
		subscription.broadcaster.remove(subscription.topic, subscription.clientID)
	})
}

func (broadcaster *Broadcaster) RetryFrame() []byte {
	return []byte(fmt.Sprintf("retry: %d\n\n", broadcaster.retryInterval.Milliseconds()))
}

func (broadcaster *Broadcaster) Close() {
	if broadcaster == nil {
		return
	}
	broadcaster.mu.Lock()
	if broadcaster.closed {
		broadcaster.mu.Unlock()
		return
	}
	broadcaster.closed = true
	for key, current := range broadcaster.topics {
		delete(broadcaster.topics, key)
		current.cancel()
		for clientID, client := range current.clients {
			delete(current.clients, clientID)
			close(client)
			broadcaster.subscribers--
		}
	}
	broadcaster.mu.Unlock()
}

func (broadcaster *Broadcaster) run(current *topic) {
	consecutiveFailures := 0
	for current.context.Err() == nil {
		value, err := current.options.Load(current.context)
		if err == nil {
			consecutiveFailures = 0
			fingerprint := current.options.Fingerprint(value)
			now := time.Now()
			if !current.hasFingerprint || fingerprint != current.lastFingerprint {
				payload, encodeErr := json.Marshal(current.options.Payload(value))
				if encodeErr != nil {
					err = encodeErr
				} else {
					current.lastFingerprint = fingerprint
					current.hasFingerprint = true
					current.lastHeartbeatAt = now
					broadcaster.broadcast(current, eventFrame(current.options.Event, payload), true)
				}
			} else if now.Sub(current.lastHeartbeatAt) >= broadcaster.heartbeatInterval {
				current.lastHeartbeatAt = now
				broadcaster.broadcast(current, []byte(fmt.Sprintf(": heartbeat %d\n\n", now.UnixMilli())), false)
			}
			if err == nil && current.options.Terminal != nil && current.options.Terminal(value) {
				break
			}
		}
		if err != nil {
			consecutiveFailures++
			payload, _ := json.Marshal(map[string]any{
				"message":  "\u4e8b\u4ef6\u6d41\u72b6\u6001\u6682\u65f6\u4e0d\u53ef\u7528\uff0c\u6b63\u5728\u91cd\u8bd5\u3002",
				"retrying": true,
			})
			broadcaster.broadcast(current, eventFrame("error", payload), false)
			delay := current.options.PollInterval * time.Duration(1<<min(6, consecutiveFailures-1))
			if delay > 30*time.Second {
				delay = 30 * time.Second
			}
			if !wait(current.context, delay) {
				break
			}
			continue
		}
		if !wait(current.context, current.options.PollInterval) {
			break
		}
	}
	broadcaster.finish(current)
}

func (broadcaster *Broadcaster) broadcast(current *topic, frame []byte, cache bool) {
	broadcaster.mu.Lock()
	defer broadcaster.mu.Unlock()
	if broadcaster.topics[current.key] != current {
		return
	}
	if cache {
		current.lastFrame = cloneFrame(frame)
	}
	for clientID, client := range current.clients {
		select {
		case client <- cloneFrame(frame):
		default:
			delete(current.clients, clientID)
			close(client)
			broadcaster.subscribers--
		}
	}
	if len(current.clients) == 0 {
		delete(broadcaster.topics, current.key)
		current.cancel()
	}
}

func (broadcaster *Broadcaster) remove(current *topic, clientID uint64) {
	broadcaster.mu.Lock()
	defer broadcaster.mu.Unlock()
	client, exists := current.clients[clientID]
	if !exists {
		return
	}
	delete(current.clients, clientID)
	close(client)
	broadcaster.subscribers--
	if len(current.clients) == 0 {
		if broadcaster.topics[current.key] == current {
			delete(broadcaster.topics, current.key)
		}
		current.cancel()
	}
}

func (broadcaster *Broadcaster) finish(current *topic) {
	broadcaster.mu.Lock()
	defer broadcaster.mu.Unlock()
	if broadcaster.topics[current.key] == current {
		delete(broadcaster.topics, current.key)
	}
	current.cancel()
	for clientID, client := range current.clients {
		delete(current.clients, clientID)
		close(client)
		broadcaster.subscribers--
	}
}

func eventFrame(event string, payload []byte) []byte {
	prefix := ""
	if event != "" {
		prefix = "event: " + event + "\n"
	}
	return []byte(prefix + "data: " + string(payload) + "\n\n")
}

func unavailable(detail string) error {
	return apperror.New(
		apperror.CodeDependencyUnavailable,
		detail,
		apperror.WithMetadata(map[string]any{"retryAfterSeconds": 5}),
	)
}

func wait(ctx context.Context, duration time.Duration) bool {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func validateDuration(value, minimum, maximum time.Duration, label string) error {
	if value < minimum || value > maximum {
		return fmt.Errorf("SSE %s must be between %s and %s", label, minimum, maximum)
	}
	return nil
}

func cloneFrame(frame []byte) []byte {
	return append([]byte(nil), frame...)
}

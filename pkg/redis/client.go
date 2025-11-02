package redis

import (
	"context"

	"github.com/redis/go-redis/v9"
)

// NewClient creates a new Redis client
func NewClient(addr string) *redis.Client {
	return redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: "", // no password set
		DB:       0,  // use default DB
	})
}

// Subscribe subscribes to a topic and handles messages with the provided handler
func Subscribe(ctx context.Context, client *redis.Client, topic string, handler func(string)) error {
	pubsub := client.Subscribe(ctx, topic)
	ch := pubsub.Channel()
	for msg := range ch {
		handler(msg.Payload)
	}
	return nil
}

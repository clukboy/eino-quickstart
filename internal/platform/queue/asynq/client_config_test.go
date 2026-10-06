package asynq

import "testing"

func TestProducerDoesNotConstructConsumer(t *testing.T) {
	// Missing concurrency/queues is deliberate: API configuration has neither.
	producer := NewProducer(&AsynqConf{Enable: true, Addr: "127.0.0.1:6379"})
	if producer.Server != nil {
		t.Fatal("producer unexpectedly owns a consumer server")
	}
	if producer.Client == nil {
		t.Fatal("producer client missing")
	}
	if err := producer.Client.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestDisabledProducerDoesNotConstructClient(t *testing.T) {
	producer := NewProducer(&AsynqConf{Enable: false})
	if producer.Client != nil || producer.Server != nil {
		t.Fatal("disabled producer constructed a client/server")
	}
}

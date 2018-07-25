package pipclient

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/infobloxopen/themis/pdp"
	pb "github.com/infobloxopen/themis/pip-service"
)

func TestStreamingClientValidation(t *testing.T) {
	s, err := newAllPermitServer("127.0.0.1:5555")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Stop()

	t.Run("fixed-buffer", testSingleRequest(WithStreams(1)))
	t.Run("auto-buffer", testSingleRequest(WithStreams(1), WithAutoRequestSize(true)))
}

func TestStreamingClientValidationWithCache(t *testing.T) {
	s, err := newAllPermitServer("127.0.0.1:5555")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Stop()

	c := NewClient(
		WithStreams(1),
		WithMaxRequestSize(128),
		WithCacheTTL(15*time.Minute),
	)
	if err := c.Connect("127.0.0.1:5555"); err != nil {
		t.Fatalf("expected no error but got %s", err)
	}
	defer c.Close()

	sc, ok := c.(*streamingClient)
	if !ok {
		t.Fatalf("expected *streamingClient but got %#v", c)
	}
	bc := sc.cache
	if bc == nil {
		t.Fatal("expected cache")
	}

	in := decisionRequest{
		Direction: "Any",
		Policy:    "AllPermitPolicy",
		Domain:    "example.com",
	}
	var out decisionResponse
	if err := c.Map(in, &out); err != nil {
		t.Errorf("expected no error but got %s", err)
	}

	if out.Effect != pdp.EffectPermit || out.Reason != nil || out.X != "AllPermitRule" {
		t.Errorf("got unexpected response: %s", out)
	}

	if bc.Len() == 1 {
		if it := bc.Iterator(); it.SetNext() {
			ei, err := it.Value()
			if err != nil {
				t.Errorf("can't get value from cache: %s", err)
			} else if err := fillResponse(pb.Msg{Body: ei.Value()}, &out); err != nil {
				t.Errorf("can't unmarshal response from cache: %s", err)
			} else if out.Effect != pdp.EffectPermit || out.Reason != nil || out.X != "AllPermitRule" {
				t.Errorf("got unexpected response from cache: %s", out)
			}
		} else {
			t.Error("can't set cache iterator to the first value")
		}
	} else {
		t.Errorf("expected the only record in cache but got %d", bc.Len())
	}

	if err := c.Map(in, &out); err != nil {
		t.Errorf("expected no error but got %s", err)
	}

	if out.Effect != pdp.EffectPermit || out.Reason != nil || out.X != "AllPermitRule" {
		t.Errorf("got unexpected response: %s", out)
	}
}

func TestStreamingClientValidationWithRoundRobingBalancer(t *testing.T) {
	firstPDP, err := newAllPermitServer("127.0.0.1:5555")
	if err != nil {
		t.Fatal(err)
	}
	defer firstPDP.Stop()

	secondPDP, err := newAllPermitServer("127.0.0.1:5556")
	if err != nil {
		t.Fatal(err)
	}
	defer secondPDP.Stop()

	c := NewClient(
		WithStreams(2),
		WithRoundRobinBalancer(
			"127.0.0.1:5555",
			"127.0.0.1:5556",
		))
	if err := c.Connect(""); err != nil {
		t.Fatalf("expected no error but got %s", err)
	}
	defer c.Close()

	in := decisionRequest{
		Direction: "Any",
		Policy:    "AllPermitPolicy",
		Domain:    "example.com",
	}
	var out decisionResponse
	if err := c.Map(in, &out); err != nil {
		t.Errorf("expected no error but got %s", err)
	}

	if out.Effect != pdp.EffectPermit || out.Reason != nil || out.X != "AllPermitRule" {
		t.Errorf("got unexpected response: %s", out)
	}
}

func TestStreamingClientValidationWithHotSpotBalancer(t *testing.T) {
	firstPDP, err := newAllPermitServer("127.0.0.1:5555")
	if err != nil {
		t.Fatal(err)
	}
	defer firstPDP.Stop()

	secondPDP, err := newAllPermitServer("127.0.0.1:5556")
	if err != nil {
		t.Fatal(err)
	}
	defer secondPDP.Stop()

	c := NewClient(
		WithStreams(2),
		WithHotSpotBalancer(
			"127.0.0.1:5555",
			"127.0.0.1:5556",
		))
	if err := c.Connect(""); err != nil {
		t.Fatalf("expected no error but got %s", err)
	}
	defer c.Close()

	in := decisionRequest{
		Direction: "Any",
		Policy:    "AllPermitPolicy",
		Domain:    "example.com",
	}

	errs := make([]error, 10)
	var wg sync.WaitGroup
	for i := range errs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()

			var out decisionResponse
			if err := c.Map(in, &out); err != nil {
				errs[i] = err
			} else if out.Effect != pdp.EffectPermit || out.Reason != nil || out.X != "AllPermitRule" {
				errs[i] = fmt.Errorf("got unexpected response: %#v", out)
			}
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("requset %d failed with error %s", i, err)
		}
	}
}

func TestStreamingClientValidationNoConnectionZeroTimeout(t *testing.T) {
	c := NewClient(
		WithStreams(1),
		WithConnectionTimeout(0),
	)
	err := c.Connect("127.0.0.1:5555")
	if err != nil {
		t.Fatalf("expected no error but got %s", err)
	}
	defer c.Close()

	done := make(chan bool)

	go func() {
		in := decisionRequest{
			Direction: "Any",
			Policy:    "AllPermitPolicy",
			Domain:    "example.com",
		}
		var out decisionResponse
		err = c.Map(in, &out)
		if err != nil {
			if err != ErrorNotConnected {
				t.Errorf("expected not connected error but got %s", err)
			}
		} else {
			t.Errorf("expected error but got response: %#v", out)
		}

		close(done)
	}()

	select {
	case <-time.After(10 * time.Second):
		t.Errorf("expected no connection error but got nothing after 10 seconds")
		c.Close()

	case <-done:
	}
}

func TestStreamingClientValidationNoConnectionTimeout(t *testing.T) {
	c := NewClient(
		WithStreams(1),
		WithConnectionTimeout(3*time.Second),
	)
	err := c.Connect("127.0.0.1:5555")
	if err != nil {
		t.Fatalf("expected no error but got %s", err)
	}
	defer c.Close()

	done := make(chan bool)

	go func() {
		in := decisionRequest{
			Direction: "Any",
			Policy:    "AllPermitPolicy",
			Domain:    "example.com",
		}
		var out decisionResponse
		err = c.Map(in, &out)
		if err != nil {
			if err != ErrorNotConnected {
				t.Errorf("expected not connected error but got %s", err)
			}
		} else {
			t.Errorf("expected error but got response: %#v", out)
		}

		close(done)
	}()

	select {
	case <-time.After(10 * time.Second):
		t.Errorf("expected no connection error but got nothing after 10 seconds")
		c.Close()

	case <-done:
	}
}

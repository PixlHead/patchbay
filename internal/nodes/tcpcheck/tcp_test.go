package tcpcheck

import (
	"context"
	"errors"
	"io"
	"net"
	"strings"
	"testing"
	"testing/synctest"
	"time"
	"unicode/utf8"

	"patchbay/internal/nodes/resulttext"
	"patchbay/internal/workflow"
)

func tcpStep(host string, port int) workflow.Step {
	return workflow.Step{
		ID: "connect", Name: "Connect", Type: "tcp.check",
		Config: workflow.CheckConfig{Host: host, Port: port, TimeoutMS: 100},
	}
}

func TestTCPConnectsAndClosesWithoutSendingData(t *testing.T) {
	listener, err := net.ListenTCP("tcp4", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	step := tcpStep("127.0.0.1", listener.Addr().(*net.TCPAddr).Port)
	step.Config.TimeoutMS = 2000
	result, err := New().Execute(context.Background(), step)
	if err != nil || !result.Healthy || result.Reason != "TCP connection established" || result.DurationMS < 0 {
		t.Fatalf("unexpected successful connection result: %+v, %v", result, err)
	}
	if result.Type != "tcp.check" || result.Host != step.Config.Host || result.Port != step.Config.Port || result.URL != "" || result.ExpectedStatus != 0 || result.StatusCode != 0 {
		t.Fatalf("TCP result lost its target or included HTTP data: %+v", result)
	}
	// A TCP handshake can complete in the listener's queue before Accept.
	// The peer must already be closed, with no application data sent.
	if err := listener.SetDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatal(err)
	}
	connection, err := listener.AcceptTCP()
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	if err := connection.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatal(err)
	}
	var data [1]byte
	if n, err := connection.Read(data[:]); n != 0 || !errors.Is(err, io.EOF) {
		t.Fatalf("check left its connection open or sent data: bytes=%d error=%v", n, err)
	}
}

func TestTCPRefusedConnectionIsHealthData(t *testing.T) {
	listener, err := net.ListenTCP("tcp4", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	step := tcpStep("127.0.0.1", listener.Addr().(*net.TCPAddr).Port)
	step.Config.TimeoutMS = 2000
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	result, err := New().Execute(context.Background(), step)
	if err != nil || result.Healthy || !strings.HasPrefix(result.Reason, "Connection failed: ") {
		t.Fatalf("refused connection was not unhealthy data: %+v, %v", result, err)
	}
	if result.Type != "tcp.check" || result.Host != step.Config.Host || result.Port != step.Config.Port {
		t.Fatalf("failed check lost its TCP target: %+v", result)
	}
}

func TestTCPDialFailuresAndAddressFormatting(t *testing.T) {
	for _, test := range []struct {
		name, host, address, reason string
		port                        int
		dialError                   error
	}{
		{
			name: "DNS failure", host: "missing.invalid", port: 443, address: "missing.invalid:443", reason: "no such host",
			dialError: &net.DNSError{Err: "no such host", Name: "missing.invalid", IsNotFound: true},
		},
		{
			name: "IPv6 address", host: "2001:db8::1", port: 8443, address: "[2001:db8::1]:8443", reason: "fixture refusal",
			dialError: errors.New("fixture refusal"),
		},
		{
			name: "bounded reason", host: "service.invalid", port: 22, address: "service.invalid:22", reason: "... [truncated]",
			dialError: errors.New(strings.Repeat("界", resulttext.MaxReasonBytes)),
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			node := New()
			node.dial = func(_ context.Context, network, address string) (net.Conn, error) {
				if network != "tcp" || address != test.address {
					t.Errorf("wrong dial target: network=%q address=%q", network, address)
				}
				return nil, test.dialError
			}
			result, err := node.Execute(context.Background(), tcpStep(test.host, test.port))
			if err != nil || result.Healthy || !strings.HasPrefix(result.Reason, "Connection failed: ") || !strings.Contains(result.Reason, test.reason) {
				t.Fatalf("unexpected dial failure result: %+v, %v", result, err)
			}
			if len(result.Reason) > resulttext.MaxReasonBytes || !utf8.ValidString(result.Reason) {
				t.Fatalf("TCP reason was not bounded UTF-8: bytes=%d", len(result.Reason))
			}
		})
	}
}

func TestTCPDistinguishesStepAndParentDeadlines(t *testing.T) {
	for _, parentDeadline := range []bool{false, true} {
		name := "step deadline"
		if parentDeadline {
			name = "parent deadline"
		}
		t.Run(name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				ctx := context.Background()
				wantDuration := int64(100)
				if parentDeadline {
					var cancel context.CancelFunc
					ctx, cancel = context.WithTimeout(ctx, 50*time.Millisecond)
					defer cancel()
					wantDuration = 50
				}
				node := New()
				node.dial = func(ctx context.Context, _, _ string) (net.Conn, error) {
					<-ctx.Done() // Models either a DNS lookup or connection attempt.
					return nil, ctx.Err()
				}
				result, err := node.Execute(ctx, tcpStep("service.invalid", 443))
				if result.Healthy || result.DurationMS != wantDuration {
					t.Fatalf("dial did not respect the earlier deadline: %+v, %v", result, err)
				}
				if parentDeadline {
					if !errors.Is(err, context.DeadlineExceeded) || result.Reason != "" {
						t.Fatalf("parent deadline was reported as service health: %+v, %v", result, err)
					}
				} else if err != nil || result.Reason != "No connection within 100 ms" {
					t.Fatalf("step timeout was not unhealthy data: %+v, %v", result, err)
				}
			})
		})
	}
}

func TestTCPParentCancellationTakesPriorityAndClosesConnection(t *testing.T) {
	for _, dialFails := range []bool{false, true} {
		name := "dial succeeds"
		if dialFails {
			name = "dial fails"
		}
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			connection, peer := net.Pipe()
			defer connection.Close()
			defer peer.Close()
			if err := peer.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
				t.Fatal(err)
			}
			node := New()
			node.dial = func(dialCtx context.Context, _, _ string) (net.Conn, error) {
				cancel()
				<-dialCtx.Done()
				if dialFails {
					return connection, errors.New("connection failed during shutdown")
				}
				return connection, nil
			}
			result, err := node.Execute(ctx, tcpStep("service.invalid", 443))
			if !errors.Is(err, context.Canceled) || result.Healthy || result.Reason != "" {
				t.Fatalf("parent cancellation lost priority: %+v, %v", result, err)
			}
			var data [1]byte
			if _, err := peer.Read(data[:]); !errors.Is(err, io.EOF) {
				t.Fatalf("cancellation leaked the returned connection: %v", err)
			}
		})
	}
}

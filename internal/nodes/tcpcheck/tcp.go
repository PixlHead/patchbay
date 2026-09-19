// Package tcpcheck checks whether a TCP port accepts connections.
package tcpcheck

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"time"

	"patchbay/internal/nodes/resulttext"
	"patchbay/internal/workflow"
)

// Executor owns a dialer and supports concurrent checks.
type Executor struct {
	// Keep dialing replaceable so timeout and cancellation tests need no network.
	dial func(context.Context, string, string) (net.Conn, error)
}

// New creates a TCP executor using the standard network dialer.
func New() *Executor {
	dialer := &net.Dialer{}
	return &Executor{dial: dialer.DialContext}
}

func (n *Executor) Execute(ctx context.Context, step workflow.Step) (workflow.CheckResult, error) {
	config := step.Config
	result := workflow.CheckResult{Type: "tcp.check", Host: config.Host, Port: config.Port}
	checkCtx, cancel := context.WithTimeout(ctx, time.Duration(config.TimeoutMS)*time.Millisecond)
	defer cancel()
	// Workflow validation supplies a host and numeric port; JoinHostPort also
	// brackets IPv6 addresses. This deadline covers both DNS and connection setup.
	address := net.JoinHostPort(config.Host, strconv.Itoa(config.Port))
	start := time.Now()
	connection, err := n.dial(checkCtx, "tcp", address)
	result.DurationMS = time.Since(start).Milliseconds()
	if connection != nil {
		connection.Close() // Connectivity only: send no data and retain no socket.
	}
	if ctx.Err() != nil {
		return result, ctx.Err()
	}
	if err != nil {
		if checkCtx.Err() != nil {
			result.Reason = fmt.Sprintf("No connection within %d ms", config.TimeoutMS)
		} else {
			result.Reason = resulttext.LimitReason("Connection failed: " + err.Error())
		}
		return result, nil // Refusal, DNS failure, and timeout are service-health data.
	}
	result.Healthy = true
	result.Reason = "TCP connection established"
	return result, nil
}

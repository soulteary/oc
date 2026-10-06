/*
 * MinIO Client, (C) 2015 MinIO, Inc.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package cmd

import (
	"os"
	"os/signal"
	"sync/atomic"
	"time"
)

// trapSignals traps the registered signals and cancel the global context.
var signalExitCode atomic.Int32

func trapSignals(done <-chan struct{}, sig ...os.Signal) {
	// channel to receive signals.
	sigCh := make(chan os.Signal, 1)
	defer close(sigCh)

	// `signal.Notify` registers the given channel to
	// receive notifications of the specified signals.
	signal.Notify(sigCh, sig...)

	// Wait for the signal.
	var s os.Signal
	select {
	case s = <-sigCh:
	case <-done:
		signal.Stop(sigCh)
		return
	}

	// Once signal has been received stop signal Notify handler.
	signal.Stop(sigCh)

	var exitCode int
	switch s.String() {
	case "interrupt":
		exitCode = globalCancelExitStatus
	case "killed":
		exitCode = globalKillExitStatus
	case "terminated":
		exitCode = globalTerminatExitStatus
	default:
		exitCode = globalErrorExitStatus
	}
	signalExitCode.Store(int32(exitCode))
	globalCancel()
	// Let command defers and stream cancellation run before Main exits. A stuck
	// consumer gets an explicit failure, never a false graceful-cancellation pass.
	if !awaitCommandCleanup(done, 3*time.Second) {
		os.Exit(1)
	}

}

func awaitCommandCleanup(done <-chan struct{}, timeout time.Duration) bool {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-done:
		return true
	case <-timer.C:
		return false
	}
}

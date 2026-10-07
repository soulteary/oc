/*
 * MinIO Client (C) 2017 MinIO, Inc.
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

import "github.com/soulteary/mc/internal/notify"

// PipeChan returns a bounded FIFO. Producers block once capacity is exhausted;
// notify uses nonblocking sends and may drop events rather than growing memory.
// Closing the input allows consumers to drain queued events and then finish.
// No forwarding goroutines are needed, so abandoning a consumer cannot leak one.
func PipeChan(capacity int) (inputCh chan notify.EventInfo, outputCh chan notify.EventInfo) {
	ch := make(chan notify.EventInfo, capacity)
	return ch, ch
}

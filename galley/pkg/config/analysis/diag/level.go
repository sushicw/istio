// Copyright 2019 Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package diag

// Level is the severity level of a message.
type Level rune

const (
	// Info level is for informational messages
	Info Level = 'I'

	// Warning level is for warning messages
	Warning Level = 'W'

	// Error level is for error messages
	Error Level = 'E'
)

// String implements io.Stringer
func (l Level) String() string {
	return string(l)
}

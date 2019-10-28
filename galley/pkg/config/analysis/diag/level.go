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

import (
	"fmt"
	"strings"
)

// Level is the severity level of a message.
type Level struct {
	sortOrder int
	name      string
}

func (l Level) String() string {
	return l.name
}

func (l Level) IsWorseThanOrEqualTo(target Level) bool {
	return l.sortOrder <= target.sortOrder
}

func LevelFromString(s string) (Level, error) {
	val, ok := GetUppercaseStringToLevelMap()[strings.ToUpper(s)]
	if !ok {
		return Level{}, fmt.Errorf("%q not a valid option, please choose from: %v", s, GetAllLevelStrings())
	}

	return val, nil
}

var (
	// Info level is for informational messages
	Info = Level{2, "Info"}

	// Warning level is for warning messages
	Warning = Level{1, "Warn"}

	// Error level is for error messages
	Error = Level{0, "Error"}
)

func GetAllLevels() []Level {
	return []Level{Info, Warning, Error}
}

func GetAllLevelStrings() []string {
	levels := GetAllLevels()
	var s []string
	for _, l := range levels {
		s = append(s, l.name)
	}
	return s
}

func GetUppercaseStringToLevelMap() map[string]Level {
	m := make(map[string]Level)
	for _, l := range GetAllLevels() {
		m[strings.ToUpper(l.name)] = l
	}
	return m
}

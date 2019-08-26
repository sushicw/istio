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

// +build ignore

package main

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"strings"
	"text/template"

	"github.com/ghodss/yaml"
)

// Utility for generating staticinit.gen.go. Called from gen.go
func main() {
	if len(os.Args) != 3 {
		fmt.Printf("Invalid args: %v", os.Args)
		os.Exit(-1)
	}

	input := os.Args[1]
	output := os.Args[2]

	m, err := read(input)
	if err != nil {
		fmt.Printf("Error reading metadata: %v", err)
		os.Exit(-2)
	}

	code, err := generate(m)
	if err != nil {
		fmt.Printf("Error generating code: %v", err)
		os.Exit(-3)
	}

	if err = ioutil.WriteFile(output, []byte(code), os.ModePerm); err != nil {
		fmt.Printf("Error writing output file: %v", err)
		os.Exit(-4)
	}
}

func read(path string) (*messages, error) {
	b, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("unable to read input file: %v", err)
	}

	m := &messages{}

	if err := yaml.Unmarshal(b, m); err != nil {
		return nil, err
	}

	return m, nil
}

var tmpl = `
// GENERATED FILE -- DO NOT EDIT
//

package msg

import (
	"istio.io/istio/galley/pkg/config/analysis/diag"
	"istio.io/istio/galley/pkg/config/resource"
)

{{range .Messages}}
// {{.FuncName}} returns a new diag.Message for message "{{.Name}}".
//
// {{.Description}}
func {{.FuncName}}(entry *resource.Entry{{range .Args}}, {{.Name}} {{.Type}}{{end}}) diag.Message {
	var o resource.Origin
	if entry != nil {
		o = entry.Origin
	}
	return diag.NewMessage(
		diag.{{.Level}},
		diag.Code({{.Code}}),
		o,
		"{{.Template}}",{{range .Args}}
		{{.Name}},
{{end}}	)
}
{{end}}
`

func generate(m *messages) (string, error) {
	t := template.Must(template.New("code").Parse(tmpl))

	var b bytes.Buffer
	if err := t.Execute(&b, m); err != nil {
		return "", err
	}
	return b.String(), nil
}

type messages struct {
	Messages []message `json:"messages"`
}

type message struct {
	Name        string `json:"name"`
	Code        int    `json:"code"`
	Level       string `json:"level"`
	Description string `json:"description"`
	Template    string `json:"template"`
	Args        []arg  `json:"args"`
}

// FuncName creates a function name from the message name
func (m *message) FuncName() string {
	return strings.Replace(m.Name, " ", "", -1)
}

type arg struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

// Copyright © 2016 NAME HERE <EMAIL ADDRESS>
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

package cmd

import (
	"errors"
	"fmt"
	"os"
	"path"
	"strings"
	"text/scanner"
	"text/template"

	"github.com/Sirupsen/logrus"
	"github.com/fsouza/go-dockerclient"
	"github.com/spf13/cobra"
)

func parse(src string) (string, []string, error) {
	var s scanner.Scanner
	s.Init(strings.NewReader(src))
	var tok rune

	types := []string{}
	ret := ""
	for tok != scanner.EOF {
		tok = s.Scan()
		logrus.Debugf("token: %s", s.TokenText())
		if s.TokenText() == "(" {
			tok = s.Scan()
			logrus.Debugf("token: %s", s.TokenText())
			for s.TokenText() != ")" {
				if tok != scanner.Ident {
					return "", nil, errors.New("Ident expected")
				}
				types = append(types, s.TokenText())
				logrus.Debugf("types: %s", types)
				tok = s.Scan()
				logrus.Debugf("token: %s", s.TokenText())
				if s.TokenText() != "," && s.TokenText() != ")" {
					return "", nil, errors.New(", or ) expected")
				}
				if s.TokenText() == ")" {
					break
				}
				tok = s.Scan()
			}
			tok = s.Scan()
			ret = s.TokenText()
		}
	}

	all := []string{}
	args := []string{}
	for i, each := range types {
		all = append(all, fmt.Sprintf("a%d %s", i, each))
		switch each {
		case "string":
			args = append(args, fmt.Sprintf("a%d", i))
		case "int", "int64":
			args = append(args, fmt.Sprintf("strconv.FormatInt(int64(a%d), 10)", i))
		default:
			panic("type " + each + " not supported yet")
		}
	}

	return fmt.Sprintf("(%s) %s", strings.Join(all, ", "), ret), args, nil
}

// getCmd represents the get command
var getCmd = &cobra.Command{
	Use:     "get [IMAGE]",
	Short:   "Get a Docker image and wrap it into a function.",
	Example: "  faas get docker.io/faas/example",
	Run: func(cmd *cobra.Command, args []string) {

		if len(args) != 1 {
			cmd.Println(`faas: "get" requires only 1 argument.
See 'faas get --help'.
`)
			cmd.Usage()
			os.Exit(255)
		}

		image := args[0]
		cli, err := docker.NewClientFromEnv()
		if err != nil {
			cmd.Println(err)
			os.Exit(2)
		}

		cmd.Printf("Pulling image from: %q ...\n", image)

		cli.PullImage(docker.PullImageOptions{Repository: image}, docker.AuthConfiguration{})
		img, err := cli.InspectImage(image)
		if err != nil {
			cmd.Println(err)
			os.Exit(3)
		}

		signature, exist := img.Config.Labels["signature"]
		if exist {

			cmd.Printf("Found signature: %q ...\n", signature)

			signature, args, _ := parse(signature)
			logrus.Debugf("signature: %s", signature)
			dir := path.Join(os.Getenv("GOPATH"), "src", path.Dir(image))
			os.MkdirAll(dir, 0755)
			content := `
// Generated by Faas.
// Do not edit !!
package {{.pkg}}

import (
	"fmt"
	"strconv"

	"github.com/fsouza/go-dockerclient"
	"github.com/chanwit/go-dexec"
)

func {{.func_name}}{{.signature}} {
	cl, err := docker.NewClientFromEnv()
	d := dexec.Docker{cl}
	executor, _ := dexec.ByCreatingContainer(
	docker.CreateContainerOptions{Config: &docker.Config{Image: "{{.pkg}}/{{.name}}"}})
	args := []string{}
	{{ range .args }}
	args = append(args, {{.}})
	{{ end }}
	cmd := d.Command(executor, args...)
	b, err := cmd.Output()
	fmt.Print(string(b))
	return err
}`
			t, err := template.New("out").Parse(content)
			if err != nil {
				cmd.Println(err)
				os.Exit(255)
			}

			data := make(map[string]interface{})
			data["import"] = path.Dir(image)
			data["pkg"] = path.Base(path.Dir(image))
			data["func_name"] = strings.Title(path.Base(image))
			data["name"] = path.Base(image)
			data["signature"] = signature
			data["args"] = args

			filename := path.Join(os.Getenv("GOPATH"), "src", image+".go")
			fout, err := os.Create(filename)
			defer fout.Close()

			if err != nil {
				cmd.Println(err)
				os.Exit(255)
			}

			err = t.Execute(fout, data)
			if err != nil {
				cmd.Println(err)
				os.Exit(255)
			}

			cmd.Printf("Wrapper written to %q.\n", filename)
			cmd.Printf(`
Add the import line to use it in a Go program,
  import (
      %q
  )
Then, for example, you can call:
  err := %s.%s(...)
			`, data["import"], data["pkg"], data["func_name"])

		} else {
			cmd.Printf("Signature not found in the image: %q\n", args[0])
			os.Exit(255)
		}
	},
}

func init() {
	if os.Getenv("DEBUG") == "1" {
		logrus.SetLevel(logrus.DebugLevel)
	}

	RootCmd.AddCommand(getCmd)

	// Here you will define your flags and configuration settings.

	// Cobra supports Persistent Flags which will work for this command
	// and all subcommands, e.g.:
	// getCmd.PersistentFlags().String("foo", "", "A help for foo")

	// Cobra supports local flags which will only run when this command
	// is called directly, e.g.:
	// getCmd.Flags().BoolP("toggle", "t", false, "Help message for toggle")

}

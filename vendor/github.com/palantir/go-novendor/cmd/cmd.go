// Copyright 2016 Palantir Technologies, Inc.
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
	"github.com/palantir/godel/framework/pluginapi"
	"github.com/palantir/pkg/cobracli"
	"github.com/spf13/cobra"

	"github.com/palantir/go-novendor/novendor"
)

var (
	rootCmd = &cobra.Command{
		Use:   "novendor [flags] [packages]",
		Short: "verifies that all vendored packages are referenced in the project",
		RunE: func(cmd *cobra.Command, args []string) error {
			config := novendor.Config{
				PkgRegexps:                pkgRegexpsFlagVal,
				IncludeVendorInImportPath: includeVendorImportPathFlagVal,
				IgnorePkgs:                ignorePkgsFlagVal,
			}
			param, err := config.ToParam()
			if err != nil {
				return err
			}
			return novendor.Run(projectDirFlagVal, args, param, cmd.OutOrStdout())
		},
	}

	projectDirFlagVal              string
	pkgRegexpsFlagVal              []string
	includeVendorImportPathFlagVal bool
	ignorePkgsFlagVal              []string

	defaultPkgRegexps = []string{
		`github\.com/[^/]+/[^/]+`,
		`golang\.org/[^/]+/[^/]+`,
		`gopkg\.in/[^/]+`,
		`github\.[^/]+/[^/]+/[^/]+`,
	}
)

func Execute() int {
	return cobracli.ExecuteWithDefaultParams(rootCmd)
}

func init() {
	pluginapi.AddProjectDirPFlagPtr(rootCmd.Flags(), &projectDirFlagVal)
	rootCmd.Flags().StringArrayVar(&pkgRegexpsFlagVal, "pkg-regexp", defaultPkgRegexps, "regular expressions used to group packages")
	rootCmd.Flags().BoolVar(&includeVendorImportPathFlagVal, "full-import-path", false, "print the full import path (including the vendor directory) for unused packages")
	rootCmd.Flags().StringSliceVar(&ignorePkgsFlagVal, "ignore-pkg", nil, "packages that should be ignored (suppressed from output)")
}

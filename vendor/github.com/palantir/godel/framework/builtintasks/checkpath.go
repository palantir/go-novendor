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

package builtintasks

import (
	"github.com/nmiyake/pkg/dirs"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"

	"github.com/palantir/godel/framework/builtintasks/checkpath"
	"github.com/palantir/godel/framework/godellauncher"
)

func CheckPathTask() godellauncher.Task {
	var checkpathApply bool
	cmd := &cobra.Command{
		Use:   checkpath.CmdName,
		Short: "Verify that the Go environment is set up properly and that the project is in the proper location",
		RunE: func(cmd *cobra.Command, args []string) error {
			wd, err := dirs.GetwdEvalSymLinks()
			if err != nil {
				return errors.Wrapf(err, "failed to determine working directory")
			}
			return checkpath.VerifyProject(wd, checkpathApply, cmd.OutOrStdout())
		},
	}
	cmd.Flags().BoolVar(&checkpathApply, checkpath.ApplyFlagName, false, "Apply the recommended changes")
	return godellauncher.CobraCLITask(cmd)
}

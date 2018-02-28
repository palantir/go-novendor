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

package novendor_test

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"regexp"
	"testing"

	"github.com/nmiyake/pkg/dirs"
	"github.com/nmiyake/pkg/gofiles"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/palantir/go-novendor/novendor"
)

const currPkgName = "github.com/palantir/go-novendor/novendor"

func TestNovendor(t *testing.T) {
	tmpDir, cleanup, err := dirs.TempDir(".", "")
	defer cleanup()
	require.NoError(t, err)

	for i, tc := range []struct {
		name              string
		files             []gofiles.GoFileSpec
		pkgs              func(projectDir string) []string
		ignorePkgs        func(projectDir string) []string
		regexps           []*regexp.Regexp
		want              string
		wantIncludeVendor func(projectDir string) string
	}{
		{
			name: "package with no dependencies",
			files: []gofiles.GoFileSpec{
				{
					RelPath: "foo.go",
					Src:     `package main`,
				},
			},
			pkgs: func(projectDir string) []string {
				return []string{
					projectDir + "/.",
				}
			},
		},
		{
			name: "package with vendored import that is used",
			files: []gofiles.GoFileSpec{
				{
					RelPath: "foo.go",
					Src:     `package main; import _ "{{index . "vendor/github.com/org/product/bar/bar.go"}}";`,
				},
				{
					RelPath: "vendor/github.com/org/product/bar/bar.go",
					Src:     `package bar`,
				},
			},
			pkgs: func(projectDir string) []string {
				return []string{
					projectDir + "/.",
				}
			},
		},
		{
			name: "multi-level vendored imports: import a non-external package that uses vendoring to import a package that is visible to the non-external package but not to the base package",
			files: []gofiles.GoFileSpec{
				{
					RelPath: "foo.go",
					Src:     `package main; import _ "{{index . "bar/bar.go"}}";`,
				},
				{
					RelPath: "bar/bar.go",
					Src:     `package bar; import _ "{{index . "bar/vendor/github.com/org/product/baz/baz.go"}}";`,
				},
				{
					RelPath: "bar/vendor/github.com/org/product/baz/baz.go",
					Src:     `package baz`,
				},
			},
			pkgs: func(projectDir string) []string {
				return []string{
					projectDir + "/.",
					projectDir + "/bar",
				}
			},
		},
		{
			// package imports vendored package that contains files that declare a "foo" and "main" package,
			// but "main" package is excluded using build constraint. The "foo" package imports another
			// package, which is also vendored. The vendored package should not be reported as unused.
			name: "simple multi-package case",
			files: []gofiles.GoFileSpec{
				{
					RelPath: "main.go",
					Src:     `package main; import _ "github.com/foo";`,
				},
				{
					RelPath: "vendor/github.com/foo/foo.go",
					Src:     `package foo; import _ "github.com/bar"`,
				},
				{
					RelPath: "vendor/github.com/foo/main.go",
					Src: `// +build ignore

package main`,
				},
				{
					RelPath: "vendor/github.com/bar/bar.go",
					Src:     `package bar`,
				},
			},
			pkgs: func(projectDir string) []string {
				return []string{
					projectDir + "/.",
				}
			},
		},
		{
			// vendored import has 3 different packages in it: "library" (2 files), "main" (1 file) and
			// "library2" (1 file), where "main" and "library" both have ignore build directives and all of
			// these packages vendor different packages. The logic for multi-package build directive parsing
			// should ensure that none of the packages vendored by the 3 different packages are reported as
			// unused.
			name: "complicated case of multi-package vendored import with build constraints",
			files: []gofiles.GoFileSpec{
				{
					RelPath: "main.go",
					Src:     `package main; import _ "github.com/org/library";`,
				},
				{
					RelPath: "vendor/github.com/org/library/library1.go",
					Src:     `package library; import _ "github.com/lib1import"`,
				},
				{
					RelPath: "vendor/github.com/lib1import/import.go",
					Src:     `package lib1import`,
				},
				{
					RelPath: "vendor/github.com/org/library/library1_too.go",
					Src:     `package library; import _ "github.com/anotherlib1import"`,
				},
				{
					RelPath: "vendor/github.com/anotherlib1import/import.go",
					Src:     `package anotherlib1import`,
				},
				{
					RelPath: "vendor/github.com/org/library/main.go",
					Src: `// +build ignore

package main; import _ "github.com/mainimport"`,
				},
				{
					RelPath: "vendor/github.com/mainimport/import.go",
					Src:     `package mainimport`,
				},
				{
					RelPath: "vendor/github.com/org/library/library2.go",
					Src: `// +build ignore

package library2; import _ "github.com/lib2import"`,
				},
				{
					RelPath: "vendor/github.com/lib2import/import.go",
					Src:     `package lib2import`,
				},
			},
			pkgs: func(projectDir string) []string {
				return []string{
					projectDir + "/.",
				}
			},
		},
		{
			// primary package has 3 different packages in it: "foo" (1 file, 1 test and 1 external test),
			// "main" (1 file) and "other" (1 file), where "main" and "other" both have ignore build
			// directives and all of these packages (and tests) vendor different packages. The logic for
			// multi-package build directive parsing should ensure that none of the packages vendored by the
			// 3 different packages and tests are reported as unused.
			name: "complicated case of multi-package package with build constraints with tests",
			files: []gofiles.GoFileSpec{
				{
					RelPath: "foo.go",
					Src:     `package foo; import _ "github.com/fooimport";`,
				},
				{
					RelPath: "foo_ext_test.go",
					Src:     `package foo_test; import _ "github.com/fooexttestimport";`,
				},
				{
					RelPath: "foo_test.go",
					Src:     `package foo; import _ "github.com/footestimport";`,
				},
				{
					RelPath: "main.go",
					Src: `// +build ignore

package main; import _ "github.com/mainimport";`,
				},
				{
					RelPath: "other.go",
					Src: `// +build ignore

package other; import _ "github.com/otherimport";`,
				},
				{
					RelPath: "vendor/github.com/fooimport/fooimport.go",
					Src:     `package fooimport`,
				},
				{
					RelPath: "vendor/github.com/fooexttestimport/fooexttestimport.go",
					Src:     `package fooexttestimport`,
				},
				{
					RelPath: "vendor/github.com/footestimport/footestimport.go",
					Src:     `package footestimport`,
				},
				{
					RelPath: "vendor/github.com/mainimport/mainimport.go",
					Src:     `package mainimport`,
				},
				{
					RelPath: "vendor/github.com/otherimport/otherimport.go",
					Src:     `package otherimport`,
				},
			},
			pkgs: func(projectDir string) []string {
				return []string{
					projectDir + "/.",
				}
			},
		},
		{
			// vendored import has 2 different packages in it: "library" (2 files) and "main". One of the "library"
			// files uses CGo, and the "main" file is excluded using a build constraint. The logic for multi-package
			// build directive parsing should ensure that the package vendored by "library" is not reported as unused.
			// This tests a logical edge case: when parsing "github.com/org/library", the first pass will report
			// "library1.go" as valid and "library1_too.go" and "main.go" as invalid. In the next pass,
			// "library1_too.go" and "main.go" will both be processed, but both will be reported as invalid. If the
			// logic does not break at this point, it can result in an infinite loop.
			name: "multi-package vendored import with build constraints including CGo",
			files: []gofiles.GoFileSpec{
				{
					RelPath: "main.go",
					Src:     `package main; import _ "github.com/org/library";`,
				},
				{
					RelPath: "vendor/github.com/org/library/library1.go",
					Src:     `package library; import _ "github.com/lib1import"`,
				},
				{
					RelPath: "vendor/github.com/lib1import/import.go",
					Src:     `package lib1import`,
				},
				{
					RelPath: "vendor/github.com/org/library/library1_too.go",
					Src: `// +build cgo

package library; import "C";`,
				},
				{
					RelPath: "vendor/github.com/org/library/main.go",
					Src: `// +build ignore

package main`,
				},
			},
			pkgs: func(projectDir string) []string {
				return []string{
					projectDir + "/.",
				}
			},
		},
		{
			name: "unused vendored package causes error",
			files: []gofiles.GoFileSpec{
				{
					RelPath: "foo.go",
					Src:     `package main`,
				},
				{
					RelPath: "vendor/github.com/org/library/subpackage/bar.go",
					Src:     `package bar`,
				},
			},
			pkgs: func(projectDir string) []string {
				return []string{
					projectDir + "/.",
				}
			},
			want: `github.com/org/library/subpackage
`,
			wantIncludeVendor: func(projectDir string) string {
				return fmt.Sprintf(`%s/%s/vendor/github.com/org/library/subpackage
`, currPkgName, projectDir)
			},
		},
		{
			name: "one subpackage of a vendored library is used but another is not -- subpackage is reported as unused",
			files: []gofiles.GoFileSpec{
				{
					RelPath: "foo.go",
					Src:     `package main; import _ "{{index . "vendor/github.com/org/library/subpackage/bar.go"}}";`,
				},
				{
					RelPath: "vendor/github.com/org/library/subpackage/bar.go",
					Src:     `package bar`,
				},
				{
					RelPath: "vendor/github.com/org/library/subpackage-unused/baz.go",
					Src:     `package baz`,
				},
			},
			pkgs: func(projectDir string) []string {
				return []string{
					projectDir + "/.",
				}
			},
			want: `github.com/org/library/subpackage-unused
`,
			wantIncludeVendor: func(projectDir string) string {
				return fmt.Sprintf(`%s/%s/vendor/github.com/org/library/subpackage-unused
`, currPkgName, projectDir)
			},
		},
		{
			name: "one subpackage of a vendored library is used but another is not -- subpackage is not reported as unused if grouped by package",
			files: []gofiles.GoFileSpec{
				{
					RelPath: "foo.go",
					Src:     `package main; import _ "{{index . "vendor/github.com/org/library/subpackage/bar.go"}}";`,
				},
				{
					RelPath: "vendor/github.com/org/library/subpackage/bar.go",
					Src:     `package bar`,
				},
				{
					RelPath: "vendor/github.com/org/library/subpackage-unused/baz.go",
					Src:     `package baz`,
				},
			},
			pkgs: func(projectDir string) []string {
				return []string{
					projectDir + "/.",
				}
			},
			regexps: []*regexp.Regexp{
				regexp.MustCompile(`github\.com/[^/]+/[^/]+`),
			},
		},
		{
			name: "computes dependencies with all build tags as true",
			files: []gofiles.GoFileSpec{
				{
					RelPath: "foo.go",
					// imports the "github.com/org/library/bar package
					Src: `package main; import _ "{{index . "vendor/github.com/org/library/bar/bar_d.go"}}";`,
				},
				// build constraints makes it such that using default build context will import either "github.com/org/library/subpackage-darwin"
				// or "github.com/org/library/subpackage-linux", but not both.
				{
					RelPath: "vendor/github.com/org/library/bar/bar_d.go",
					Src: `// +build darwin

package bar; import _ "{{index . "vendor/github.com/org/library/subpackage_darwin/darwin_go_pkg.go"}}";`,
				},
				{
					RelPath: "vendor/github.com/org/library/bar/bar_l.go",
					Src: `// +build linux

package bar; import _ "{{index . "vendor/github.com/org/library/subpackage_linux/linux_go_pkg.go"}}";`,
				},
				{
					RelPath: "vendor/github.com/org/library/subpackage_darwin/darwin_go_pkg.go",
					Src:     `package red`,
				},
				{
					RelPath: "vendor/github.com/org/library/subpackage_linux/linux_go_pkg.go",
					Src:     `package blue`,
				},
			},
			pkgs: func(projectDir string) []string {
				return []string{
					projectDir + "/.",
				}
			},
		},
		{
			name: "does not consider vendored libraries in hidden directories",
			files: []gofiles.GoFileSpec{
				{
					RelPath: "foo.go",
					Src:     `package main`,
				},
				{
					RelPath: ".hidden/vendor/github.com/org/library/bar/bar.go",
					Src:     `package bar`,
				},
			},
			pkgs: func(projectDir string) []string {
				return []string{
					projectDir + "/.",
				}
			},
		},
		{
			name: "considers multiple vendor directories",
			files: []gofiles.GoFileSpec{
				{
					RelPath: "foo.go",
					Src:     `package main`,
				},
				{
					RelPath: "vendor/github.com/org/library/bar/bar.go",
					Src:     `package bar`,
				},
				{
					RelPath: "subdir/vendor/github.com/org/library/bar/bar.go",
					Src:     `package bar`,
				},
			},
			pkgs: func(projectDir string) []string {
				return []string{
					projectDir + "/.",
					projectDir + "/subdir",
				}
			},
			want: `github.com/org/library/bar
github.com/org/library/bar
`,
			wantIncludeVendor: func(projectDir string) string {
				return fmt.Sprintf(`%s/%s/subdir/vendor/github.com/org/library/bar
%s/%s/vendor/github.com/org/library/bar
`, currPkgName, projectDir, currPkgName, projectDir)
			},
		},
		{
			// verifies that import paths are examined in a fully qualified manner. In example below,
			// "zgithub.com/org/lib1" exists in 2 different vendor directories (and are referenced in both). A previous
			// bug recorded "zgithub.com/org/lib1" as examined (in addition to the fully qualified
			// "{{pathToVendorDir}}/zgithub.com/org/lib1"), which would cause only one of the imports to be recorded and
			// thus one of the packages was reported as unused.
			name: "handles multiple vendor directories where imports resolve into directories",
			files: []gofiles.GoFileSpec{
				{
					RelPath: "foo.go",
					Src:     `package main; import _ "zgithub.com/org/lib1"; import _ "{{index . "subdir/bar.go"}}";`,
				},
				{
					RelPath: "vendor/zgithub.com/org/lib1/lib1.go",
					Src:     `package lib1; //import _ "zgithub.com/org/lib2";`,
				},
				{
					RelPath: "subdir/bar.go",
					Src:     `package bar; import _ "zgithub.com/org/lib1";`,
				},
				{
					RelPath: "subdir/vendor/zgithub.com/org/lib1/lib1.go",
					Src:     `package lib1;// import _ "zgithub.com/org/lib2";`,
				},
			},
			pkgs: func(projectDir string) []string {
				return []string{
					projectDir + "/.",
					projectDir + "/subdir",
				}
			},
			want: ``,
			wantIncludeVendor: func(projectDir string) string {
				return ""
			},
		},
		{
			name: "ignore specified package and its dependencies",
			files: []gofiles.GoFileSpec{
				{
					RelPath: "foo.go",
					Src:     `package main`,
				},
				{
					RelPath: "vendor/github.com/org/library/bar/bar.go",
					Src:     `package bar; import _ "github.com/org/other-lib/foo";`,
				},
				{
					RelPath: "vendor/github.com/org/other-lib/foo/foo.go",
					Src:     `package foo`,
				},
				{
					RelPath: "subdir/vendor/github.com/org/library/bar/bar.go",
					Src:     `package bar`,
				},
			},
			pkgs: func(projectDir string) []string {
				return []string{
					projectDir + "/.",
					projectDir + "/subdir",
				}
			},
			ignorePkgs: func(projectDir string) []string {
				return []string{
					projectDir + "/vendor/github.com/org/library/bar",
				}
			},
			want: `github.com/org/library/bar
`,
			wantIncludeVendor: func(projectDir string) string {
				return fmt.Sprintf(`%s/%s/subdir/vendor/github.com/org/library/bar
`, currPkgName, projectDir)
			},
		},
		{
			name: "ignore multiple packages",
			files: []gofiles.GoFileSpec{
				{
					RelPath: "foo.go",
					Src:     `package main`,
				},
				{
					RelPath: "vendor/github.com/org/library/bar/bar.go",
					Src:     `package bar; import _ "github.com/org/other-lib/foo";`,
				},
				{
					RelPath: "vendor/github.com/org/other-lib/foo/foo.go",
					Src:     `package foo`,
				},
				{
					RelPath: "subdir/vendor/github.com/org/library/bar/bar.go",
					Src:     `package bar`,
				},
			},
			pkgs: func(projectDir string) []string {
				return []string{
					projectDir + "/.",
					projectDir + "/subdir",
				}
			},
			ignorePkgs: func(projectDir string) []string {
				return []string{
					projectDir + "/vendor/github.com/org/library/bar",
					projectDir + "/subdir/vendor/github.com/org/library/bar",
				}
			},
		},
	} {
		projectDir, err := ioutil.TempDir(tmpDir, "")
		require.NoError(t, err, "Case %d (%s)", i, tc.name)

		_, err = gofiles.Write(projectDir, tc.files)
		require.NoError(t, err, "Case %d (%s)", i, tc.name)

		// run in regular mode
		param := novendor.Param{}
		param.PkgRegexps = tc.regexps
		if tc.ignorePkgs != nil {
			param.IgnorePkgs = tc.ignorePkgs(projectDir)
		}

		buf := &bytes.Buffer{}
		err = novendor.Run(projectDir, tc.pkgs(projectDir), param, buf)
		require.NoError(t, err, "Case %d (%s)", i, tc.name)
		assert.Equal(t, tc.want, buf.String(), "Case %d (%s)", i, tc.name)

		// run with includeVendor set to true
		param.IncludeVendorInImportPath = true
		buf = &bytes.Buffer{}
		err = novendor.Run(projectDir, tc.pkgs(projectDir), param, buf)
		require.NoError(t, err, "Case %d (%s)", i, tc.name)

		wantIncludeVendor := ""
		if tc.wantIncludeVendor != nil {
			wantIncludeVendor = tc.wantIncludeVendor(projectDir)
		}
		assert.Equal(t, wantIncludeVendor, buf.String(), "Case %d (%s)", i, tc.name)
	}
}

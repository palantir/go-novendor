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

package novendor

import (
	"fmt"
	"go/build"
	"io"
	"io/ioutil"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/pkg/errors"
)

type Config struct {
	PkgRegexps                []string `json:"pkgRegexps"`
	IncludeVendorInImportPath bool     `json:"includeVendorInImportPath"`
	IgnorePkgs                []string `json:"ignorePkgs"`
}

func (c *Config) ToParam() (Param, error) {
	regexps, err := regexpsForPkgMatchers(c.PkgRegexps)
	if err != nil {
		return Param{}, err
	}
	return Param{
		PkgRegexps:                regexps,
		IncludeVendorInImportPath: c.IncludeVendorInImportPath,
		IgnorePkgs:                c.IgnorePkgs,
	}, nil
}

type Param struct {
	PkgRegexps                []*regexp.Regexp
	IncludeVendorInImportPath bool
	IgnorePkgs                []string
}

func Run(projectDir string, pkgs []string, param Param, w io.Writer) error {
	unusedPkgs, err := unusedVendoredPackages(projectDir, pkgs, param.IgnorePkgs, param.PkgRegexps)
	if err != nil {
		return err
	}

	var out []string
	for _, v := range unusedPkgs {
		out = append(out, sortedVals(v)...)
	}

	if !param.IncludeVendorInImportPath {
		for i, importPath := range out {
			vendorIdx := strings.LastIndex(importPath, "/vendor/")
			if vendorIdx == -1 {
				continue
			}
			out[i] = importPath[vendorIdx+len("/vendor/"):]
		}
	}
	sort.Strings(out)

	for _, pkg := range out {
		fmt.Fprintln(w, pkg)
	}
	return nil
}

func sortedVals(in map[string]struct{}) []string {
	var out []string
	for k := range in {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func unusedVendoredPackages(projectDir string, pkgs, ignorePkgs []string, regexps []*regexp.Regexp) (map[string]map[string]struct{}, error) {
	wd, err := os.Getwd()
	if err != nil {
		return nil, errors.Wrapf(err, "failed to determine working directory")
	}

	if !filepath.IsAbs(projectDir) {
		projectDir = path.Join(wd, projectDir)
	}

	absPkgPaths := toAbsPaths(pkgs, wd)
	vendorDirs := make(map[string]map[string]struct{})
	for _, pkgPath := range absPkgPaths {
		vendorDirPath := path.Join(pkgPath, "vendor")
		if fi, err := os.Stat(vendorDirPath); err != nil || !fi.IsDir() {
			continue
		}

		pkgsInVendorDir, err := allVendoredPackages(vendorDirPath)
		if err != nil {
			return nil, errors.Wrapf(err, "failed to determine packages in vendor directory %s", vendorDirPath)
		}
		normalizedPkgImportPaths := make(map[string]struct{})
		for pkg := range pkgsInVendorDir {
			normalizedPkgImportPaths[transformImportPath(pkg, regexps)] = struct{}{}
		}
		vendorDirs[vendorDirPath] = normalizedPkgImportPaths
	}

	// add ignore packages to absPkgPaths so that packages to ignore (and all their dependencies) are not considered.
	// Done here instead of earlier because vendor directories in the ignore packages should not be considered.
	absPkgPaths = append(absPkgPaths, toAbsPaths(ignorePkgs, wd)...)
	for _, pkgPath := range absPkgPaths {
		importsInPkg, err := allImportsInPkg(pkgPath, projectDir)
		if err != nil {
			return nil, errors.Wrapf(err, "failed to determine imports in package %s", pkgPath)
		}
		for currImportPath := range importsInPkg {
			normalizedImportPath := transformImportPath(currImportPath, regexps)
			for _, vendorDirPkgs := range vendorDirs {
				delete(vendorDirPkgs, normalizedImportPath)
			}
		}
	}
	return vendorDirs, nil
}

func toAbsPaths(in []string, wd string) []string {
	var out []string
	for _, pkgPath := range in {
		if !filepath.IsAbs(pkgPath) {
			pkgPath = path.Join(wd, pkgPath)
		}
		out = append(out, pkgPath)
	}
	return out
}

// regexpsForPkgMatchers returns the compiled regular expressions for the provided inputs. If the input regular
// expression does not start with "^", it is added to ensure that a prefix match occurs. Returns an error if any of the
// provided expressions do not compile.
func regexpsForPkgMatchers(exprs []string) ([]*regexp.Regexp, error) {
	var regexps []*regexp.Regexp
	for _, expr := range exprs {
		if !strings.HasPrefix(expr, "^") {
			expr = "^" + expr
		}
		compiled, err := regexp.Compile(expr)
		if err != nil {
			return nil, errors.Wrapf(err, "failed to compile expression %s", expr)
		}
		regexps = append(regexps, compiled)
	}
	return regexps, nil
}

// transformImportPath takes the provided import path and normalizes it if it matches any of the provided regular
// expressions. This function is used to map an import path to a normalized "repository" or "project" for the input
// path. If the import path includes "/vendor/", then the normalization occurs for the portion of the path after the
// last occurrence of "/vendor/". If the import path matches a provided regular expression, the matching part is
// replaced with just the match for the regular expression.
//
// Examples:
//   "github.com/org/project/inner/pkg", `^github.com/[^/]+/[^/]+` -> "github.com/org/project"
//   "github.com/org/project/vendor/gopkg.in/yaml.v2/inner", `^gopkg.in/[^/]+` -> "github.com/org/project/vendor/gopkg.in/yaml.v2"
func transformImportPath(importPath string, regexps []*regexp.Regexp) string {
	vendorPrefix := ""
	if lastVendorIdx := strings.LastIndex(importPath, "/vendor/"); lastVendorIdx != -1 {
		idxAfterLastVendor := lastVendorIdx + len("/vendor/")
		vendorPrefix = importPath[:idxAfterLastVendor]
		importPath = importPath[idxAfterLastVendor:]
	}
	for _, reg := range regexps {
		if match := reg.FindStringSubmatch(importPath); len(match) > 0 {
			importPath = match[0]
			break
		}
	}
	return vendorPrefix + importPath
}

// allVendoredPackages returns the import paths of all of the packages in the provided vendor directory. The provided
// input must be the path to a directory named "vendor". The returned import paths include the vendor directory itself.
// For example, if the vendor directory is in a package with the import path "github.com/org/repo" and contains
// "github.com/org/vendored", then the returned map would contain "github.com/org/repo/vendor/github.com/org/vendored".
// Packages in the vendor directory are determined without regard to build constraints.
func allVendoredPackages(vendorDir string) (map[string]struct{}, error) {
	vendorDirAbsPath := vendorDir
	if !filepath.IsAbs(vendorDir) {
		wd, err := os.Getwd()
		if err != nil {
			return nil, errors.Wrapf(err, "failed to determine working directory")
		}
		vendorDirAbsPath = path.Join(wd, vendorDir)
	}

	if path.Base(vendorDirAbsPath) != "vendor" {
		return nil, errors.Errorf("provided path must be a directory named 'vendor', was %s", vendorDirAbsPath)
	}
	if fi, err := os.Stat(vendorDirAbsPath); err != nil {
		return nil, errors.Wrapf(err, "failed to stat %s", vendorDirAbsPath)
	} else if !fi.IsDir() {
		return nil, errors.Errorf("path %s is not a directory", vendorDirAbsPath)
	}

	pkgImportPaths := make(map[string]struct{})
	if err := filepath.Walk(vendorDirAbsPath, func(path string, info os.FileInfo, err error) error {
		if !info.IsDir() {
			return nil
		}

		buildPkgs, err := getPkgsInDir(".", path, make(map[string]struct{}))
		if err != nil {
			return errors.Wrapf(err, "failed to get packages in directory %s", path)
		}

		dirContainsPkg := false
		for _, pkg := range buildPkgs {
			if pkg.Name != "" {
				dirContainsPkg = true
				break
			}
		}

		if !dirContainsPkg {
			return nil
		}

		pkgImportPaths[buildPkgs[0].ImportPath] = struct{}{}
		return nil
	}); err != nil {
		return nil, errors.Wrapf(err, "failed to walk directory")
	}
	return pkgImportPaths, nil
}

func allImportsInPkg(pkgDir, projectDir string) (map[string]struct{}, error) {
	imps, err := getAllImports(".", pkgDir, projectDir, make(map[string]struct{}), true)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to get all imports for package in directory %s in project %s", pkgDir, projectDir)
	}
	return imps, nil
}

// getAllImports takes an import and returns all of the packages that it imports (excluding standard library packages).
// Includes all transitive imports and the package of the import itself. Assumes that the import occurs in a package in
// "srcDir". If the "test" parameter is "true", considers all imports in the test files for the package as well.
func getAllImports(importPkgPath, srcDir, projectRoot string, examinedImports map[string]struct{}, includeTests bool) (map[string]struct{}, error) {
	importedPkgs := make(map[string]struct{})

	pkgs, err := getPkgsInDir(importPkgPath, srcDir, examinedImports)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to get packages in package %s", importPkgPath)
	}

	origSrcDir := srcDir
	for _, pkg := range pkgs {
		importedPkgs[pkg.ImportPath] = struct{}{}
		examinedImports[pkg.ImportPath] = struct{}{}

		currPkgImports := pkg.Imports
		if rel, err := filepath.Rel(projectRoot, pkg.Dir); err == nil && !strings.HasPrefix(rel, "../") {
			// if import is internal, update "srcDir" to be pkg.Dir to ensure that resolution is done against the
			// last internal package that was encountered
			srcDir = pkg.Dir
			if includeTests {
				// if import is internal and includeTests is true, consider imports from test files
				currPkgImports = append(currPkgImports, pkg.TestImports...)
				currPkgImports = append(currPkgImports, pkg.XTestImports...)
			}
		}

		// add packages from imports (don't examine transitive test dependencies)
		for _, currImport := range currPkgImports {
			if _, ok := examinedImports[currImport]; ok {
				continue
			}

			currImportedPkgs, err := getAllImports(currImport, srcDir, projectRoot, examinedImports, false)
			if err != nil {
				return nil, errors.Wrapf(err, "failed to get all imports for %s", currImport)
			}

			for k, v := range currImportedPkgs {
				importedPkgs[k] = v
			}
		}
		// restore srcDir to original value in case it was updated to pkg.Dir within loop
		srcDir = origSrcDir
	}
	return importedPkgs, nil
}

func getPkgsInDir(importPkgPath, srcDir string, examinedImports map[string]struct{}) ([]*build.Package, error) {
	if !strings.Contains(importPkgPath, ".") {
		// if package is a standard package, return empty
		return nil, nil
	}

	var pkgs []*build.Package
	ctxIgnoreFiles := make(map[string]struct{})
	for {
		// ignore error because doImport returns partial object even on error. As long as an ImportPath is present,
		// proceed with determining imports. Perform the import using the provided ctxIgnoreFiles.
		pkg, pkgErr := doImport(importPkgPath, srcDir, build.ImportComment, ctxIgnoreFiles)
		if pkg.ImportPath == "" {
			break
		}

		// skip if package has already been examined
		if _, ok := examinedImports[pkg.ImportPath]; ok {
			break
		}

		if _, ok := pkgErr.(*build.MultiplePackageError); !ok {
			// only one package in directory: add it and finish
			pkgs = append(pkgs, pkg)
			break
		}

		// current package path has multiple packages

		// create set of invalid files
		invalidFilesMap := make(map[string]struct{})
		for _, currInvalid := range pkg.InvalidGoFiles {
			invalidFilesMap[currInvalid] = struct{}{}
		}

		// create set of valid Go files (Go files that were not considered invalid)
		validGoFiles := make(map[string]struct{})
		for _, currFile := range append(append(pkg.GoFiles, pkg.TestGoFiles...), pkg.XTestGoFiles...) {
			if _, ok := invalidFilesMap[currFile]; ok {
				continue
			}
			validGoFiles[currFile] = struct{}{}
		}

		if len(validGoFiles) == 0 {
			// remaining files are not considered valid, so don't bother continuing. This can happen if a single
			// directory has multiple packages but none of the individual packages are valid.
			break
		}

		if pkg, _ := doImport(importPkgPath, srcDir, build.ImportComment, combineMaps(ctxIgnoreFiles, invalidFilesMap)); pkg.ImportPath != "" {
			pkgs = append(pkgs, pkg)
		}

		// ignore files that were processed in this iteration in next iteration
		ctxIgnoreFiles = combineMaps(ctxIgnoreFiles, validGoFiles)
	}
	return pkgs, nil
}

func combineMaps(m1, m2 map[string]struct{}) map[string]struct{} {
	out := make(map[string]struct{})
	for k, v := range m1 {
		out[k] = v
	}
	for k, v := range m2 {
		out[k] = v
	}
	return out
}

// allContext is a build.Context based on build.Default that has "UseAllFiles" set to true. Makes it such that analysis
// is done on all Go files rather than on just those that match the default build context.
var allContext = getAllContext()

func getAllContext() build.Context {
	ctx := build.Default
	ctx.UseAllFiles = true
	return ctx
}

// doImport performs an "Import" operation. If "ignoreFiles" does not have any entries, it uses "allContext" to do the
// import. Otherwise, it creates a new "all" context with a custom ReadDir function that ignores files with the names in
// the provided map.
func doImport(path, srcDir string, mode build.ImportMode, ignoreFiles map[string]struct{}) (*build.Package, error) {
	if len(ignoreFiles) == 0 {
		return allContext.Import(path, srcDir, mode)
	}

	ctx := getAllContext()
	ctx.ReadDir = func(dir string) ([]os.FileInfo, error) {
		files, err := ioutil.ReadDir(dir)
		var filesToReturn []os.FileInfo
		for _, curr := range files {
			if _, ok := ignoreFiles[curr.Name()]; ok {
				continue
			}
			filesToReturn = append(filesToReturn, curr)
		}
		return filesToReturn, err
	}
	return ctx.Import(path, srcDir, mode)
}

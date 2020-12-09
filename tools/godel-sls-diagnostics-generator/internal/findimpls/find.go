// Copyright (c) 2020 Palantir Technologies. All rights reserved.
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

package findimpls

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/packages"
)

type Query struct {
	// packages.Load options
	LoadConfig *packages.Config
	Packages   []string

	// interface to find implementations

	InterfacePackage string
	InterfaceName    string

	// Methods subset to extract syntax
	Methods []string
}

type ResultPackages map[*packages.Package]ResultTypes

type ResultTypes map[types.Type]ResultMethods

type ResultMethods map[string]*ast.FuncDecl

// Find searches through packages specified in the Query for the InterfacePackage/InterfaceName provided.
// Once the interface is found, all packages are searched for types which implement the interface.
// The returned map has a possibly-empty entry for all packages so that absence can be easily asserted.
func Find(query *Query) (ResultPackages, error) {
	// ensure we have all the required modes
	query.LoadConfig.Mode |= packages.NeedDeps | packages.NeedImports | packages.NeedFiles | packages.NeedName | packages.NeedSyntax | packages.NeedTypes | packages.NeedTypesInfo
	if query.LoadConfig.Fset == nil {
		query.LoadConfig.Fset = token.NewFileSet()
	}
	loadedPkgs, err := packages.Load(query.LoadConfig, query.Packages...)
	if err != nil {
		return nil, fmt.Errorf("failed to load project packages: %v", err)
	}

	ifaceType, err := findInterface(loadedPkgs, query.InterfacePackage, query.InterfaceName)
	if err != nil {
		return nil, err
	}

	result := make(ResultPackages, len(loadedPkgs))
	for _, pkg := range loadedPkgs {
		impls, err := findInterfaceImplementations(ifaceType.Underlying().(*types.Interface), pkg)
		if err != nil {
			return nil, err
		}
		pkgResult, err := loadPkgTypeMethodASTs(impls, pkg, query.Methods)
		if err != nil {
			return nil, err
		}

		result[pkg] = pkgResult
	}
	return result, nil
}

func findInterface(loadedPkgs []*packages.Package, ifaceImportPath, ifaceName string) (*types.Named, error) {
	var ifacePkg *packages.Package
	var ifaceIdent *ast.Ident
	var ifaceObject types.Object
	for i := range loadedPkgs {
		if loadedPkgs[i].ID == ifaceImportPath {
			ifacePkg = loadedPkgs[i]
			for ident, object := range ifacePkg.TypesInfo.Defs {
				if ident != nil && ident.Obj != nil && ident.Obj.Kind == ast.Typ && ident.Name == ifaceName {
					ifaceIdent = ident
					ifaceObject = object
					break
				}
			}
			if ifaceIdent == nil {
				return nil, fmt.Errorf("did not find interface type %q in loaded package %q", ifaceName, ifaceImportPath)
			}
			break
		}
	}
	if ifacePkg == nil {
		return nil, fmt.Errorf("did not find interface package %q", ifaceImportPath)
	}

	ifaceType := ifaceObject.Type().(*types.Named)
	return ifaceType, nil
}

// For every non-interface type definition in the package, check if it implements the interface and collect all matches.
func findInterfaceImplementations(iface *types.Interface, pkg *packages.Package) ([]types.Type, error) {
	var results []types.Type
	for ident, object := range pkg.TypesInfo.Defs {
		if ident != nil && ident.Obj != nil && ident.Obj.Kind == ast.Typ {
			typ := object.Type()
			// Skip other interfaces
			if _, ok := typ.Underlying().(*types.Interface); ok {
				continue
			}
			if types.Implements(typ, iface) {
				results = append(results, typ)
			} else if ptr := types.NewPointer(typ); types.Implements(ptr, iface) {
				results = append(results, ptr)
			}
		}
	}
	return results, nil
}

// Returns a mapping from each impl type to its methods
func loadPkgTypeMethodASTs(impls []types.Type, pkg *packages.Package, methodNames []string) (ResultTypes, error) {
	// Find all the methods in the package
	methodDecls := pkgMethodBodyDecls(pkg)

	result := make(ResultTypes, len(impls))
	for _, impl := range impls {
		result[impl] = make(ResultMethods, len(methodNames))
		for _, methodName := range methodNames {
			methodDecl, err := findMethodDeclAST(impl, pkg, methodName, methodDecls)
			if err != nil {
				return nil, err
			}
			result[impl][methodName] = methodDecl
		}
	}
	return result, nil
}

func findMethodDeclAST(impl types.Type, pkg *packages.Package, methodName string, pkgMethodDecls map[token.Pos]*ast.FuncDecl) (*ast.FuncDecl, error) {
	methodObj, _, _ := types.LookupFieldOrMethod(impl, true, pkg.Types, methodName)
	if methodObj == nil {
		return nil, fmt.Errorf("did not find method %s on type %s", methodName, impl.String())
	}
	method, ok := methodObj.(*types.Func)
	if !ok {
		return nil, fmt.Errorf("field %s on type %s should be a function", method, impl.String())
	}
	methodDecl, ok := pkgMethodDecls[method.Scope().Pos()]
	if !ok {
		return nil, fmt.Errorf("method %s on type %s not found in AST with matching body position", method, impl.String())
	}

	return methodDecl, nil
}

func pkgMethodBodyDecls(pkg *packages.Package) map[token.Pos]*ast.FuncDecl {
	allMethodDecls := map[token.Pos]*ast.FuncDecl{}
	for _, astFile := range pkg.Syntax {
		for _, decl := range astFile.Decls {
			declFunc, ok := decl.(*ast.FuncDecl)
			if ok && declFunc.Recv != nil {
				allMethodDecls[declFunc.Body.Pos()] = declFunc
			}
		}
	}
	return allMethodDecls
}

package main

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
)

type Enum struct {
	Values  []string
	Default string
}

type Pair struct {
	Key   string
	Value string
}

type ValidatorArgs struct {
	Required  bool
	ParamName string
	Enum      Enum
	min       int
	max       int
}

type Api struct {
	Url    string
	Auth   bool
	Method string
}

type FuncData struct {
	Struct       Pair
	MethodName   string
	Params       []Pair
	ReturnValues []string
	Api          Api
}

func main() {
	set := token.NewFileSet()
	f, err := parser.ParseFile(set, os.Args[1], nil, parser.ParseComments)
	if err != nil {
		panic(err)
	}
	res, err := os.Create(os.Args[2])
	if err != nil {
		panic(err)
	}

	funcData := make([]FuncData, 0)
	for _, decl := range f.Decls {
		funcDecl, ok := decl.(*ast.FuncDecl)
		if !ok {
			fmt.Println("It is not func. Skip")
			continue
		}

		apigenString, containsApigen := getApigenString(funcDecl.Doc)
		if !containsApigen {
			fmt.Println("It is not apigen. Skip")
			continue
		}

		apigen := new(Api)
		substr := apigenString[strings.IndexRune(apigenString, '{'):]
		fmt.Println("Found apigen: ", substr)
		bytes := []byte(substr)
		err := json.Unmarshal(bytes, apigen)
		if err != nil {
			panic(err)
		}

		params := make([]Pair, 0)
		for _, param := range funcDecl.Type.Params.List {
			var value string
			ident, ok := param.Type.(*ast.Ident)

			if !ok {
				selector, ok := param.Type.(*ast.SelectorExpr)
				if !ok {
					fmt.Println("Something went wrong with params casting")
					continue
				}
				value = selector.Sel.Name + "." + selector.X.(*ast.Ident).Name
			} else {
				value = ident.Name
			}

			params = append(params, Pair{
				Key:   param.Names[0].Name,
				Value: value,
			})
		}

		funcData = append(funcData, FuncData{
			Api:        *apigen,
			Struct:     Pair{funcDecl.Recv.List[0].Names[0].Name, funcDecl.Recv.List[0].Type.(*ast.StarExpr).X.(*ast.Ident).Name},
			MethodName: funcDecl.Name.Name,
			Params:     params,
		})
	}

	fmt.Fprintln(res, "package "+f.Name.Name)
	fmt.Println(funcData)
}

func getApigenString(doc *ast.CommentGroup) (string, bool) {
	if doc == nil {
		return "", false
	}

	comments := doc.List
	for _, comment := range comments {
		if strings.Contains(comment.Text, "apigen") {
			return comment.Text, true
		}
	}
	return "", false
}

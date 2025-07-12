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

const (
	httpServe = "func (h *%s) ServeHTTP(w http.ResponseWriter, r *http.Request) {\n"
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
	Recv         *ast.Field
	MethodName   string
	Params       []*ast.Field
	ReturnValues []*ast.Field
	Api          Api
}

type FileData struct {
	FuncData    []FuncData
	PackageName string
}

func main() {
	data := extractData()
	mapData := groupByStructLink(data)
	res, err := os.Create(os.Args[2])
	if err != nil {
		panic(err)
	}

	fmt.Fprintln(res, "package "+data.PackageName)
	fmt.Fprintln(res, "\nimport (\n\"net/http\"\n\"encoding/json\"\n)\n")
	for k, v := range mapData {
		fmt.Fprintf(res, httpServe, k)
		fmt.Fprintln(res, "\tswitch r.URL.Path {")
		for _, funcData := range v {
			fmt.Fprintf(res, "\t\tcase \"%s\":\n", funcData.Api.Url)
			fmt.Fprintf(res, "\t\t\tconverted := convertFor%s(r.URL.RawQuery)\n", funcData.MethodName) //TODo пока только для GET метода
			fmt.Fprintf(res, "\t\t\tres, _ := h.%s(nil, converted)\n", funcData.MethodName)
			fmt.Fprintf(res, "\t\t\tjsonRes, _ := json.Marshal(res)\n")
			fmt.Fprintf(res, "\t\t\tw.Write(jsonRes)\n")
		}
		fmt.Fprintln(res, "\t}")
		fmt.Fprintln(res, "}\n")

		for _, funcData := range v {
			fmt.Fprintf(res, "func convertFor%s(params string) %s {\n", funcData.MethodName, funcData.Params[1].Type.(*ast.Ident).Name)

			fmt.Fprintln(res, "}\n")
		}
	}

	fmt.Println(data.FuncData)
}

func groupByStructLink(data FileData) map[string][]FuncData {
	mapData := make(map[string][]FuncData)
	for _, datum := range data.FuncData {
		name := datum.Recv.Type.(*ast.StarExpr).X.(*ast.Ident).Name
		funcData, ext := mapData[name]
		if !ext {
			funcData = []FuncData{datum}
			mapData[name] = funcData
		} else {
			mapData[name] = append(funcData, datum)
		}
	}
	return mapData
}

func extractData() FileData {
	set := token.NewFileSet()
	f, err := parser.ParseFile(set, os.Args[1], nil, parser.ParseComments)
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

		funcData = append(funcData, FuncData{
			Api:        *apigen,
			Recv:       funcDecl.Recv.List[0],
			MethodName: funcDecl.Name.Name,
			Params:     funcDecl.Type.Params.List,
		})
	}

	data := FileData{
		FuncData:    funcData,
		PackageName: f.Name.Name,
	}
	return data
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

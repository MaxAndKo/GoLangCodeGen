package main

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strconv"
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
	HasEnum   bool
	Min       int
	HasMin    bool
	Max       int
	HasMax    bool
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

var (
	putRes = `
func putRes(res interface{}) []byte {
	jsonRes, _ := json.Marshal(map[string]interface{}{
		"error":    "",
		"response": res,
	})
	return jsonRes
}
`
	processPostBody = `		var params string
		if r.Method == http.MethodPost {
			all, _ := io.ReadAll(r.Body)
			params = string(all)
		} else {
			params = r.URL.RawQuery
		}
`
	ifValidationError = `		if error != nil {
		http.Error(w, "{\"error\":\"" + error.Error() + "\"}", http.StatusBadRequest)
		return
		}
`
	ifProcessError = `		if error != nil {
		if error.Error() == "user not exist" {
			http.Error(w, "{\"error\":\""+error.Error()+"\"}", http.StatusNotFound)
			return
		}
		http.Error(w, "{\"error\":\""+error.Error()+"\"}", http.StatusInternalServerError)
		return
		}
`
	ifWrongUrl = `	http.Error(w, "{\"error\":\"unknown method\"}", http.StatusNotFound)
`
	ifApiError = `		apiError, ok := error.(ApiError)
			if ok {
				http.Error(w, "{\"error\":\""+apiError.Error()+"\"}", apiError.HTTPStatus)
				return
			}
`
)

func main() {
	data := extractData()
	mapData := groupByStructLink(data)
	res, err := os.Create(os.Args[2])
	if err != nil {
		panic(err)
	}

	fmt.Fprintln(res, "package "+data.PackageName)
	fmt.Fprintln(res, "\nimport (\n\"net/http\"\n\"encoding/json\"\n\"strings\"\n\"strconv\"\n\"errors\"\n\"slices\"\n\"io\"\n)\n")
	for k, v := range mapData {
		fmt.Fprintf(res, httpServe, k)
		fmt.Fprintln(res, "\turl := r.URL.Path")
		for _, funcData := range v {
			fmt.Fprintf(res, "\tif url == \"%s\"{\n", funcData.Api.Url)
			if funcData.Api.Method != "" {
				fmt.Fprintf(res, "\t\tif r.Method != \"%s\" {\n\t\t\thttp.Error(w, \"{\\\"error\\\":\\\"bad method\\\"}\", http.StatusNotAcceptable)\n\t\t\treturn\n\t\t}\n", funcData.Api.Method)
			}
			if funcData.Api.Auth {
				fmt.Fprintf(res, "\t\tauthToken := r.Header.Get(\"X-Auth\")\n\t\tif authToken == \"\" {\n\t\t\thttp.Error(w, \"{\\\"error\\\":\\\"unauthorized\\\"}\", http.StatusForbidden)\n\t\t\treturn\n\t\t}\n")
			}
			fmt.Fprintf(res, processPostBody)
			fmt.Fprintf(res, "\t\tconverted, error := convertFor%s%s(params)\n", k, funcData.MethodName)
			fmt.Fprintf(res, ifValidationError)
			fmt.Fprintf(res, "\t\tres, error := h.%s(nil, converted)\n", funcData.MethodName)
			fmt.Fprintf(res, ifApiError)
			fmt.Fprintf(res, ifProcessError)
			fmt.Fprintf(res, "\t\tw.Write(putRes(res))\n")
			fmt.Fprintf(res, "\t\treturn\n")
			fmt.Fprintf(res, "\t}\n")
		}
		fmt.Fprint(res, ifWrongUrl)
		fmt.Fprintln(res, "}\n")

		for _, funcData := range v {
			convertableType := funcData.Params[1].Type.(*ast.Ident)
			fmt.Fprintf(res, "func convertFor%s%s(params string) (%s, error) {\n", k, funcData.MethodName, convertableType.Name)
			fields := convertableType.Obj.Decl.(*ast.TypeSpec).Type.(*ast.StructType).Fields.List
			for _, field := range fields {
				isInt := field.Type.(*ast.Ident).Name == "int"
				args := parseValidatorArgs(field.Tag)
				targetName := strings.ToLower(field.Names[0].Name)
				if args.ParamName != "" {
					targetName = args.ParamName
				}

				fieldName := "field" + field.Names[0].Name
				stringFieldName := "stringField" + field.Names[0].Name
				if isInt {
					fmt.Fprintf(res, "\tvar %s int\n", fieldName)
					fmt.Fprintf(res, "\t%s := ", stringFieldName)
				} else {
					fmt.Fprintf(res, "\t%s := ", fieldName)
				}
				fmt.Fprintf(res, "getStringValue(params, \"%s\")\n", targetName)

				if args.Required {
					requiredFieldName := fieldName
					if isInt {
						requiredFieldName = stringFieldName
					}
					fmt.Fprintf(res, "\tif %s == \"\" {\n\t\treturn %s{}, errors.New(\"%s must me not empty\")\n\t}\n", requiredFieldName, convertableType.Name, targetName)

				}

				requiredFieldName := fieldName
				if isInt {
					requiredFieldName = stringFieldName
				}
				if args.HasMin || args.HasMax {
					fmt.Fprintf(res, "\tif %s != \"\"{\n", requiredFieldName)
				}
				if isInt {
					fmt.Fprintf(res, "\t\tvar err error\n")
					fmt.Fprintf(res, "\t\t%s, err = strconv.Atoi(%s)\n", fieldName, stringFieldName)
					fmt.Fprintf(res, "\t\tif err != nil {\n\t\t\treturn %s{}, errors.New(\"%s must be int\")\n\t\t}\n", convertableType.Name, targetName)
				}

				if args.HasMax {
					if isInt {
						fmt.Fprintf(res, "\t\tif %s > %d{\n\t\t\treturn %s{}, errors.New(\"%s must be <= %d\")\n\t\t\t}\n", fieldName, args.Max, convertableType.Name, targetName, args.Max)
					} else {
						fmt.Fprintf(res, "\t\tif len(%s) > %d{\n\t\t\treturn %s{}, errors.New(\"%s len must be <= %d\")\n\t\t}\n", fieldName, args.Max, convertableType.Name, targetName, args.Max)
					}
				}

				if args.HasMin {
					if isInt {
						fmt.Fprintf(res, "\t\tif %s < %d{\n\t\t\treturn %s{}, errors.New(\"%s must be >= %d\")\n\t\t\t}\n", fieldName, args.Min, convertableType.Name, targetName, args.Min)
					} else {
						fmt.Fprintf(res, "\t\tif len(%s) < %d{\n\t\t\treturn %s{}, errors.New(\"%s len must be >= %d\")\n\t\t}\n", fieldName, args.Min, convertableType.Name, targetName, args.Min)
					}
				}

				if args.HasMin || args.HasMax {
					fmt.Fprintf(res, "\t}\n")
				}

				if args.HasEnum {
					if isInt {
						panic("Can't be int")
					}
					fmt.Fprintf(res, "\tif %s == \"\"{\n", fieldName)
					fmt.Fprintf(res, "\t\t%s = \"%s\"\n", fieldName, args.Enum.Default)
					fmt.Fprintf(res, "\t} else {\n")
					fmt.Fprintf(res, "\t\tif !slices.Contains([]string{\"%s\"}, %s){\n", strings.Join(args.Enum.Values, "\",\""), fieldName)
					fmt.Fprintf(res, "\t\t\treturn %s{}, errors.New(\"%s must be one of [%s]\")\n", convertableType.Name, targetName, strings.Join(args.Enum.Values, ", "))
					fmt.Fprintf(res, "\t\t}\n")
					fmt.Fprintf(res, "\t}\n")
				}

			}
			fmt.Fprintf(res, "\treturn %s{\n", convertableType.Name)

			for _, field := range fields {
				value := "field" + field.Names[0].Name
				fmt.Fprintf(res, "\t\t%s:%s,\n", field.Names[0].Name, value)
			}
			fmt.Fprintln(res, "\t}, nil\n")

			fmt.Fprintln(res, "}\n")
		}
	}
	fmt.Fprintln(res, "func getStringValue(value string, name string) string {\n\tparamnameIndex := strings.Index(value, name)\n\tif paramnameIndex != -1 {\n\t\tcuttedStart := value[paramnameIndex+len(name) + 1:]\n\t\tfirstAmpersand := strings.Index(cuttedStart, \"&\")\n\t\tif firstAmpersand == -1 {\n\t\t\treturn cuttedStart\n\t\t}\n\t\treturn cuttedStart[:firstAmpersand]\n\t} else {\n\t\treturn \"\"\n\t}\n}")
	fmt.Fprintf(res, putRes)

	fmt.Println(data.FuncData)
}

// TODO обработать ситуацию, когда нет никаких tag
func parseValidatorArgs(stringArgs *ast.BasicLit) ValidatorArgs {
	value := stringArgs.Value
	args := ValidatorArgs{}
	if strings.Contains(value, "required") {
		args.Required = true
	}

	args.ParamName = getValueFromString(value, "paramname")
	minString := getValueFromString(value, "min")
	if minString != "" {
		args.Min, _ = strconv.Atoi(minString)
		args.HasMin = true
	}
	maxString := getValueFromString(value, "max")
	if maxString != "" {
		args.Max, _ = strconv.Atoi(maxString)
		args.HasMax = true
	}

	enumString := getValueFromString(value, "enum")
	defaultString := getValueFromString(value, "default")
	if enumString != "" {
		args.Enum = Enum{
			Values:  strings.Split(enumString, "|"),
			Default: defaultString,
		}
		args.HasEnum = true
	}

	return args
}

func getValueFromString(value string, name string) string {
	paramnameIndex := strings.Index(value, name)
	if paramnameIndex != -1 {
		cuttedStart := value[paramnameIndex+len(name)+1:]
		firstDoubleQuote := strings.Index(cuttedStart, "\"")
		firstComma := strings.Index(cuttedStart, ",")
		if firstComma == -1 || firstComma > firstDoubleQuote {
			return cuttedStart[:firstDoubleQuote]
		}
		return cuttedStart[:firstComma]
	} else {
		return ""
	}
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

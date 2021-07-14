module github.com/peak/s5cmd

go 1.13

require (
	github.com/aws/aws-sdk-go v1.35.13
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/google/go-cmp v0.4.0
	github.com/hashicorp/go-multierror v1.0.0
	github.com/iancoleman/strcase v0.0.0-20191112232945-16388991a334
	github.com/johannesboyne/gofakes3 v0.0.0-20191228161223-9aee1c78a252
	github.com/karrick/godirwalk v1.15.3
	github.com/kballard/go-shellquote v0.0.0-20180428030007-95032a82bc51
	github.com/kr/pretty v0.2.0 // indirect
	github.com/posener/complete v1.2.3
	github.com/stretchr/testify v1.4.0
	github.com/termie/go-shutil v0.0.0-20140729215957-bcacb06fecae
	github.com/urfave/cli/v2 v2.2.0
	golang.org/x/sys v0.0.0-20190422165155-953cdadca894 // indirect
	golang.org/x/text v0.3.2 // indirect
	gopkg.in/check.v1 v1.0.0-20180628173108-788fd7840127 // indirect
	gotest.tools/v3 v3.0.2
)

replace github.com/johannesboyne/gofakes3 => github.com/igungor/gofakes3 v0.0.6
replace github.com/urfave/cli/v2 => github.com/ocakhasan/cli/v2 v2.4.0

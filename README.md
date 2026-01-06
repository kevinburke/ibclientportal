# IB Client Portal

This is a partial API designed to work with the Interactive Brokers Client
Portal API.

To get started you want to download the Interactive Brokers "gw" tool, a Java
app, that handles authentication for you. Then you can make requests against
that API.

```
cd /path/to/clientportal.gw
./bin/run.sh ./root/conf.yaml

running
 runtime path : root:dist/ibgroup.web.core.iblink.router.clientportal.gw.jar:build/lib/runtime/*
 verticle     :
 -> mount demo on /demo
Java Version: 19.0.2
****************************************************
version: ed4af2592e9dd4a784d5403843bd18292fd441ea Fri, 9 Nov 2018 13:23:18 -0500
****************************************************
This is a Beta release of the Client Portal Gateway
for any issues, please contact api@ibkr.com
and include a copy of your logs
****************************************************
https://www.interactivebrokers.com/api/doc.html
****************************************************
Open https://localhost:5000 to login
```

Note only one Interactive Brokers session can be active at one time and it's
designed not to be automated. This will be annoying, I guarantee it.

## Usage

```go
import "github.com/kevinburke/ibclientportal"

func main() {
	client := ibclientportal.New("") // defaults to https://localhost:5000
	client.SetInsecureSkipVerify()   // this is bad; you probably want to remove
	contracts, err := client.Contracts.Stocks(ctx, url.Values{
		"symbols": []string{"VOO", "VT"},
	})
	// ...
}
```

## Testing

Some tests exercise live endpoints. To override the default host used in tests,
set `IBCLIENTPORTAL_TEST_HOST`:

```sh
IBCLIENTPORTAL_TEST_HOST=https://localhost:5000 go test -run TestStocks
```

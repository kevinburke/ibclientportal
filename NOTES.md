## Start the IB client

```
cd ~/Downloads/clientportal.gw
./bin/run.sh root/conf.yaml
```

## API Docs

https://www.interactivebrokers.com/api/doc.html#tag/Market-Data/paths/~1iserver~1marketdata~1history/get

### Search for a symbol

$ curl -i -k https://localhost:5001/v1/api/trsrv/stocks\?symbols\=VOO
HTTP/1.1 200 OK
Referrer-Policy: Origin-when-cross-origin
x-response-time: 16ms
Content-Type: application/json;charset=utf-8
X-Content-Type-Options: nosniff
Expires: Mon, 28 Nov 2022 01:24:30 GMT
Cache-Control: max-age=0, no-cache, no-store
Pragma: no-cache
Date: Mon, 28 Nov 2022 01:24:30 GMT
Connection: keep-alive
Server-Timing: cdn-cache; desc=MISS
Server-Timing: edge; dur=59
Server-Timing: origin; dur=49
Vary: Origin
Transfer-Encoding: chunked

{
    "VOO": [
        {
            "name": "VANGUARD S&P 500 ETF",
            "chineseName": "Vanguard&#x6807;&#x666E;500 ETF",
            "assetClass": "STK",
            "contracts": [
                {
                    "conid": 136155092,
                    "exchange": "MEXI",
                    "isUS": false
                },
                {
                    "conid": 136155102,
                    "exchange": "ARCA",
                    "isUS": true
                }
            ]
        }
    ]
}

### Get historical prices

$ curl -k https://localhost:5001/v1/api/iserver/marketdata/history\?conid\=136155102\&period\=30d\&bar\=1d | python -mjson.tool
  % Total    % Received % Xferd  Average Speed   Time    Time     Time  Current
                                 Dload  Upload   Total   Spent    Left  Speed
100  2572    0  2572    0     0   7791      0 --:--:-- --:--:-- --:--:--  7841
{
    "serverId": "1628",
    "symbol": "VOO",
    "text": "VANGUARD S&P 500 ETF",
    "priceFactor": 100,
    "chartAnnotations": "x/D 1.6717/36000;",
    "startTime": "20221125-14:30:00",
    "high": "37719/40807/25920",
    "low": "34434/50070/38880",
    "timePeriod": "30d",
    "barLength": 86400,
    "mdAvailability": "S",
    "mktDataDelay": 0,
    "outsideRth": false,
    "volumeFactor": 1,
    "priceDisplayRule": 1,
    "priceDisplayValue": "2",
    "negativeCapable": false,
    "messageVersion": 2,
    "data": [
        {
            "o": 366.79,
            "c": 363.95,
            "h": 368.38,
            "l": 363.15,
            "v": 25544,
            "t": 1669645800000
        },
        {
            "o": 363.93,
            "c": 363.31,
            "h": 365.18,
            "l": 361.48,
            "v": 18703,
            "t": 1669732200000
        },
        {
            "o": 363.45,
            "c": 374.49,
            "h": 374.59,
            "l": 361.65,
            "v": 35945,
            "t": 1669818600000
        },
        {
            "o": 375.73,
            "c": 374.54,
            "h": 377.0,
            "l": 372.05,
            "v": 31251,
            "t": 1669905000000
        },
        {
            "o": 369.66,
            "c": 374.0,
            "h": 374.88,
            "l": 369.66,
            "v": 27036,
            "t": 1669991400000
        },
        {
            "o": 371.28,
            "c": 367.34,
            "h": 372.19,
            "l": 365.99,
            "v": 27070,
            "t": 1670250600000
        },
        {
            "o": 367.07,
            "c": 362.03,
            "h": 367.61,
            "l": 359.93,
            "v": 27557,
            "t": 1670337000000
        },
        {
            "o": 361.05,
            "c": 361.33,
            "h": 363.61,
            "l": 360.31,
            "v": 23960,
            "t": 1670423400000
        },
        {
            "o": 363.13,
            "c": 364.18,
            "h": 365.15,
            "l": 361.6,
            "v": 21022,
            "t": 1670509800000
        },
        {
            "o": 363.0,
            "c": 361.52,
            "h": 365.45,
            "l": 361.34,
            "v": 24740,
            "t": 1670596200000
        },
        {
            "o": 362.23,
            "c": 366.68,
            "h": 366.68,
            "l": 361.58,
            "v": 21329,
            "t": 1670855400000
        },
        {
            "o": 377.11,
            "c": 369.39,
            "h": 377.19,
            "l": 366.8,
            "v": 40807,
            "t": 1670941800000
        },
        {
            "o": 369.1,
            "c": 367.16,
            "h": 372.8,
            "l": 364.25,
            "v": 34166,
            "t": 1671028200000
        },
        {
            "o": 362.47,
            "c": 358.13,
            "h": 363.24,
            "l": 356.46,
            "v": 38044,
            "t": 1671114600000
        },
        {
            "o": 355.65,
            "c": 353.86,
            "h": 356.83,
            "l": 351.79,
            "v": 32443,
            "t": 1671201000000
        },
        {
            "o": 354.13,
            "c": 350.81,
            "h": 354.34,
            "l": 349.25,
            "v": 38167,
            "t": 1671460200000
        },
        {
            "o": 348.45,
            "c": 349.67,
            "h": 351.17,
            "l": 347.2,
            "v": 37901,
            "t": 1671546600000
        },
        {
            "o": 352.17,
            "c": 354.9,
            "h": 355.96,
            "l": 351.7,
            "v": 26737,
            "t": 1671633000000
        },
        {
            "o": 351.98,
            "c": 349.91,
            "h": 352.25,
            "l": 344.34,
            "v": 50070,
            "t": 1671719400000
        },
        {
            "o": 348.93,
            "c": 351.87,
            "h": 351.95,
            "l": 347.35,
            "v": 31974,
            "t": 1671805800000
        },
        {
            "o": 351.76,
            "c": 350.47,
            "h": 352.04,
            "l": 348.85,
            "v": 30808,
            "t": 1672151400000
        },
        {
            "o": 350.42,
            "c": 346.17,
            "h": 352.2,
            "l": 345.9,
            "v": 35639,
            "t": 1672237800000
        },
        {
            "o": 348.85,
            "c": 352.31,
            "h": 353.13,
            "l": 348.47,
            "v": 30058,
            "t": 1672324200000
        },
        {
            "o": 349.79,
            "c": 351.34,
            "h": 351.49,
            "l": 347.76,
            "v": 37283,
            "t": 1672410600000
        },
        {
            "o": 353.18,
            "c": 349.99,
            "h": 355.05,
            "l": 347.19,
            "v": 34398,
            "t": 1672756200000
        },
        {
            "o": 352.1,
            "c": 352.51,
            "h": 354.57,
            "l": 349.2,
            "v": 21371,
            "t": 1672842600000
        },
        {
            "o": 350.73,
            "c": 348.66,
            "h": 350.8,
            "l": 348.06,
            "v": 22852,
            "t": 1672929000000
        },
        {
            "o": 351.59,
            "c": 356.59,
            "h": 357.67,
            "l": 348.73,
            "v": 25791,
            "t": 1673015400000
        },
        {
            "o": 358.73,
            "c": 356.33,
            "h": 361.73,
            "l": 356.22,
            "v": 29062,
            "t": 1673274600000
        }
    ],
    "points": 28,
    "travelTime": 7
}

### Search for con id for symbol

```
> POST /v1/api/iserver/secdef/search HTTP/1.1
> Host: localhost:5000
> User-Agent: curl/7.87.0
> Accept: */*
> Content-Type: application/json
> Content-Length: 19
>
* Mark bundle as not supporting multiuse
< HTTP/1.1 200 OK
HTTP/1.1 200 OK
< Referrer-Policy: Origin-when-cross-origin
Referrer-Policy: Origin-when-cross-origin
< x-response-time: 64ms
x-response-time: 64ms
< Content-Type: application/json;charset=utf-8
Content-Type: application/json;charset=utf-8
< X-Content-Type-Options: nosniff
X-Content-Type-Options: nosniff
< Expires: Tue, 17 Jan 2023 06:44:19 GMT
Expires: Tue, 17 Jan 2023 06:44:19 GMT
< Cache-Control: max-age=0, no-cache, no-store
Cache-Control: max-age=0, no-cache, no-store
< Pragma: no-cache
Pragma: no-cache
< Date: Tue, 17 Jan 2023 06:44:19 GMT
Date: Tue, 17 Jan 2023 06:44:19 GMT
< Connection: keep-alive
Connection: keep-alive
< Server-Timing: cdn-cache; desc=MISS
Server-Timing: cdn-cache; desc=MISS
< Server-Timing: edge; dur=55
Server-Timing: edge; dur=55
< Server-Timing: origin; dur=110
Server-Timing: origin; dur=110
< Vary: Origin
Vary: Origin
< Transfer-Encoding: chunked
Transfer-Encoding: chunked

<
* Connection #0 to host localhost left intact
[{"conid":"145972066","companyHeader":"Vanguard Global Minimum Volatility Fund (Vanguard)","companyName":"Vanguard Global Minimum Volatility Fund (Vanguard)","symbol":"VMNVX","description":null,"restricted":null,"fop":null,"opt":null,"war":null,"sections":[{"secType":"FUND"}]}]
```

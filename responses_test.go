package ibclientportal

var stocksResponse = []byte(`
{"VOO":[{"name":"VANGUARD S&P 500 ETF","chineseName":"Vanguard&#x6807;&#x666E;500 ETF","assetClass":"STK","contracts":[{"conid":136155092,"exchange":"MEXI","isUS":false},{"conid":136155102,"exchange":"ARCA","isUS":true}]}],"VT":[{"name":"VANGUARD TOT WORLD STK ETF","chineseName":"&#x9886;&#x822A;&#x5168;&#x7403;&#x80A1;&#x7968;ETF","assetClass":"STK","contracts":[{"conid":52197301,"exchange":"ARCA","isUS":true}]}]}
`)

var historyMarketDataResponse = []byte(`
{
    "serverId": "8505",
    "symbol": "VOO",
    "text": "VANGUARD S&P 500 ETF",
    "priceFactor": 100,
    "startTime": "20221229-14:30:00",
    "high": "36615/34167/20160",
    "low": "34719/34398/7200",
    "timePeriod": "10d",
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
        },
        {
            "o": 355.84,
            "c": 358.87,
            "h": 358.95,
            "l": 354.96,
            "v": 22677,
            "t": 1673361000000
        },
        {
            "o": 360.41,
            "c": 363.45,
            "h": 363.51,
            "l": 359.66,
            "v": 38581,
            "t": 1673447400000
        },
        {
            "o": 364.48,
            "c": 364.81,
            "h": 366.15,
            "l": 360.6,
            "v": 34167,
            "t": 1673533800000
        }
    ],
    "points": 8,
    "travelTime": 52
}
`)

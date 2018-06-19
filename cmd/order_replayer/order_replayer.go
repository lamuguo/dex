package main

import (
	"bufio"
	"flag"
	"fmt"
	"math"
	"net/rpc"
	"os"
	"strconv"
	"strings"

	"github.com/dfinity/go-dfinity-crypto/bls"
	"github.com/helinwang/dex/pkg/consensus"
	"github.com/helinwang/dex/pkg/dex"
)

func getTokens(client *rpc.Client) ([]dex.Token, error) {
	var tokens dex.TokenState
	err := client.Call("WalletService.Tokens", 0, &tokens)
	if err != nil {
		return nil, err
	}

	return tokens.Tokens, nil
}

func nonce(client *rpc.Client, addr consensus.Addr) (uint8, uint64, error) {
	var slot dex.NonceSlot
	err := client.Call("WalletService.Nonce", addr, &slot)
	if err != nil {
		return 0, 0, err
	}

	return slot.Idx, slot.Val, nil
}

func main() {
	err := bls.Init(int(bls.CurveFp254BNb))
	if err != nil {
		panic(err)
	}

	credentialPath := flag.String("c", "", "path to the node credential file")
	orderPath := flag.String("order", "", "path to the order file to replay")
	addr := flag.String("addr", ":12001", "node's wallet RPC endpoint")
	flag.Parse()

	client, err := rpc.DialHTTP("tcp", *addr)
	if err != nil {
		panic(err)
	}

	tokens, err := getTokens(client)
	if err != nil {
		panic(err)
	}

	tokenCache := make(map[string]dex.Token)
	for _, t := range tokens {
		tokenCache[strings.ToLower(string(t.Symbol))] = t
	}

	credential, err := consensus.LoadCredential(*credentialPath)
	if err != nil {
		panic(err)
	}

	f, err := os.Open(*orderPath)
	if err != nil {
		panic(err)
	}

	defer f.Close()

	s := bufio.NewScanner(f)
	for s.Scan() {
		ss := strings.Split(s.Text(), ",")

		market := ss[0]
		ms := strings.Split(market, "_")
		if len(ms) != 2 {
			panic(fmt.Errorf("unknown market format: %s, should be BASE_QUOTE, e.g., ETH_BTC", market))
		}
		base := ms[0]
		baseToken, ok := tokenCache[strings.ToLower(base)]
		if !ok {
			panic(fmt.Errorf("unknown token: %s", base))
		}
		quote := ms[1]
		quoteToken, ok := tokenCache[strings.ToLower(quote)]
		if !ok {
			panic(fmt.Errorf("unknown token: %s", quote))
		}

		var sellSide bool
		side := ss[1]
		if side == "buy" {
			sellSide = false
		} else if side == "sell" {
			sellSide = true
		} else {
			panic(fmt.Errorf("unknown sell position: %s", side))
		}

		price, err := strconv.ParseFloat(ss[2], 64)
		if err != nil {
			panic(err)
		}

		quant, err := strconv.ParseFloat(ss[3], 64)
		if err != nil {
			panic(err)
		}

		priceMul := math.Pow10(int(dex.OrderPriceDecimals))
		priceUnit := uint64(price * priceMul)
		quantMul := math.Pow10(int(quoteToken.Decimals))
		quantUnit := uint64(quant * quantMul)

		nonceIdx, nonceVal, err := nonce(client, credential.SK.MustPK().Addr())
		if err != nil {
			panic(err)
		}

		t := dex.PlaceOrderTxn{
			SellSide:     sellSide,
			Quant:        quantUnit,
			Price:        priceUnit,
			ExpireHeight: 0,
			Market:       dex.MarketSymbol{Base: baseToken.ID, Quote: quoteToken.ID},
		}
		txn := dex.MakePlaceOrderTxn(credential.SK, t, nonceIdx, nonceVal)
		err = client.Call("WalletService.SendTxn", txn, nil)
		if err != nil {
			panic(err)
		}
	}

	if s.Err() != nil {
		panic(s.Err())
	}
}
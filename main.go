package main

import (
	"crypto/tls"
	"errors"
	"flag"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/onrik/logrus/filename"
	log "github.com/sirupsen/logrus"
	"github.com/xlk3099/ok-trading/ok"
	"github.com/xlk3099/ok-trading/utils"
)

var addr = flag.String("addr", "real.okex.com:10440", "http service address")

type state = string

func init() {
	// init logrus
	formatter := &log.TextFormatter{
		FullTimestamp:   true,
		TimestampFormat: "2006-01-02T15:04:05",
	}

	log.SetFormatter(formatter)
	// Output to stdout instead of the default stderr
	// Can be any io.Writer, see below for File example
	log.SetOutput(os.Stdout)
	filenameHook := filename.NewHook()
	filenameHook.Field = "source"
	log.AddHook(filenameHook)
}

func main() {

	http.DefaultTransport.(*http.Transport).TLSClientConfig = &tls.Config{InsecureSkipVerify: true}

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		tradeEMA15("etc_usd", "this_week")
	}()

	go func() {
		defer wg.Done()
		tradeEMA15("bch_usd", "this_week")
	}()
	wg.Wait()
	// go func() {
	// 	defer wg.Done()
	// 	updateEMA5()
	// }()
	// go func() {
	// 	defer wg.Done()
	// 	updateEMA30()
	// }()
	// wg.Wait()
}

func trade() {
	// 比较ma20, ma
}

// func updateEMA1() {
// 	// calculate ema1
// 	btc := ok.NewPair("bch_usd", "this_week", "", "")
// 	ema := func() *utils.Ema {
// 		klines := btc.GetFutureKlineData("1min")
// 		ema := utils.NewEma(12)
// 		for _, k := range klines {
// 			ema.Add(k.TimeStamp, k.Close)
// 		}
// 		return ema
// 	}
// 	log.Info("1min:", ema().Latest())
// 	ticker := time.NewTicker(1 * time.Minute)
// 	defer ticker.Stop()
// 	for {
// 		select {
// 		case <-ticker.C:
// 			ema := ema()
// 			log.Info("1min:", ema.Latest())
// 		}
// 	}
// }

func tradeEMA15(name string, contractType string) {
	pair := ok.NewPair(name, contractType)
	fma := func() *utils.Ema {
		klines := pair.GetFutureKlineData("15min")
		fma := utils.NewEma(12)
		for _, k := range klines {
			fma.Add(k.TimeStamp, k.Close)
		}
		return fma
	}
	sma := func() *utils.Ema {
		klines := pair.GetFutureKlineData("15min")
		sma := utils.NewEma(50)
		for _, k := range klines {
			sma.Add(k.TimeStamp, k.Close)
		}
		return sma
	}

	ema12 := fma()
	ema50 := sma()
	log.WithFields(log.Fields{"交易对": name, "fma": ema12.Current(), "sma": ema50.Current()}).Info("成功上线...")
	ticker5 := time.NewTicker(5 * time.Second)
	ticker1 := time.NewTicker(1 * time.Second)

	defer ticker5.Stop()
	defer ticker1.Stop()

	var fpr *ok.FuturePosResp
	var ft *ok.FutureTicker
	var currentHolding int
	// var state string

	for {
		select {
		case <-ticker1.C:
			fpr = doGetFurturePos4Fix(pair)
			ft = pair.GetFutureTicker()
			if len(fpr.Holdings) > 0 {
				hold := fpr.Holdings[0]
				tryTakeProfit(pair, ft, &hold)
			}
		case <-ticker5.C:
			ema12 = fma()
			ema50 = sma()
			userInfo := pair.GetFutureUserInfo4Fix()
			var amtToTrade int
			switch name {
			case "bch_usd":
				amtToTrade = int(userInfo.Info.Bch.Balance * 150)
			case "etc_usd":
				amtToTrade = int(userInfo.Info.Etc.Balance / 5 * 20)
			}
			// var amtToTrade = 10
			// 当前有持仓？
			if utils.IsGoldCross(ema12, ema50, ft.Ticker.Last) {
				if len(fpr.Holdings) > 0 {
					amtClose := fpr.Holdings[0].SellAvailable
					if amtClose > 0 {
						success := doTrade(pair, utils.Float64ToString(ft.Ticker.Buy), strconv.Itoa(amtClose), ok.CloseShort, true)
						if success {
							log.WithField("交易对", name).Info("大爷止损成功")
						} else {
							log.WithField("交易对", name).Error("大爷止损失败")
						}
					}
					if cha := fpr.Holdings[0].BuyAmount; cha < amtToTrade*3/4 {
						success := doTrade(pair, utils.Float64ToString(ft.Ticker.Sell), strconv.Itoa(amtToTrade-cha), ok.Long, true)
						if success {
							log.WithField("交易对", name).Info("大爷增单成功：", amtToTrade-cha)
							currentHolding = amtToTrade
						} else {
							log.WithField("交易对", name).Error("大爷增单失败")
						}
						continue
					}
				}
				if currentHolding < amtToTrade*3/4 {
					success := doTrade(pair, utils.Float64ToString(ft.Ticker.Sell), strconv.Itoa(amtToTrade-currentHolding), ok.Long, true)
					if success == true {
						log.WithField("交易对", name).Info("大爷开多成功,张数：", amtToTrade)
						currentHolding = amtToTrade
					} else {
						log.WithField("交易对", name).Error("大爷开多失败....很遗憾，检查程序bug吧。。。")
					}
				}
			}

			if utils.IsDeadCross(ema12, ema50, ft.Ticker.Last) {
				ft := pair.GetFutureTicker()
				if len(fpr.Holdings) > 0 {
					amtClose := fpr.Holdings[0].BuyAvailable
					if amtClose > 0 {
						success := doTrade(pair, utils.Float64ToString(ft.Ticker.Buy), strconv.Itoa(amtClose), ok.CloseLong, true)
						if success {
							log.WithField("交易对", name).Info("大爷止损成功")
						} else {
							log.WithField("交易对", name).Error("大爷止损失败")
						}
					}
					if cha := fpr.Holdings[0].SellAmount; cha < amtToTrade*3/4 {
						success := doTrade(pair, utils.Float64ToString(ft.Ticker.Sell), strconv.Itoa(amtToTrade-cha), ok.Short, true)
						if success {
							log.WithField("交易对", name).Info("大爷增单成功：", amtToTrade-cha)
							currentHolding = amtToTrade
						} else {
							log.WithField("交易对", name).Error("大爷增单失败")
						}
						continue
					}
				}
				if currentHolding < amtToTrade*3/4 {
					success := doTrade(pair, utils.Float64ToString(ft.Ticker.Sell), strconv.Itoa(amtToTrade-currentHolding), ok.Short, true)
					if success {
						log.WithField("交易对", name).Info("大爷开空成功", amtToTrade)
						currentHolding = amtToTrade
					} else {
						log.WithField("交易对", name).Error("大爷开空失败....很遗憾，检查程序bug吧。。。")
					}
				}
			}
		}
	}
}

func tryTakeProfit(pair *ok.Pair, ft *ok.FutureTicker, hold *ok.Holding) {
	const profitTakeRatio20 = 20.00
	const profitTakeRatio50 = 50.00
	const profitTakeRatio100 = 100.00
	const profitTakeRatio200 = 200.00

	// 检查当前仓位
	// 做空止盈
	if amtToClose := hold.SellAvailable; amtToClose > 0 {
		f, _ := strconv.ParseFloat(hold.SellProfitLossratio, 64)
		if f >= profitTakeRatio20 || f >= profitTakeRatio50 || f >= profitTakeRatio100 || f >= profitTakeRatio200 {
			success := doTrade(pair, utils.Float64ToString(ft.Ticker.Buy), strconv.Itoa(amtToClose/2), ok.CloseShort, false)
			if success == true {
				log.Info("稳得不行， 一半收益已经进入腰包。。。")
			}
		}
		return
	}
	// 做多止盈
	if amtToClose := hold.BuyAvailable; amtToClose > 0 {
		f, _ := strconv.ParseFloat(hold.SellProfitLossratio, 64)
		if f >= profitTakeRatio20 || f >= profitTakeRatio50 || f >= profitTakeRatio100 || f >= profitTakeRatio200 {
			success := doTrade(pair, utils.Float64ToString(ft.Ticker.Sell), strconv.Itoa(amtToClose/2), ok.CloseLong, false)
			if success == true {
				log.Info("稳得不行， 一半收益已经进入腰包。。。")
			}
		}
		return
	}
}

// func updateEMA30() {
// 	// calculate EMA30
// 	btc := ok.NewPair("bch_usd", "this_week", "", "")
// 	ema := func() *utils.Ema {
// 		klines := btc.GetFutureKlineData("30min")
// 		ema := utils.NewEma(12)
// 		for _, k := range klines {
// 			ema.Add(k.TimeStamp, k.Close)
// 		}
// 		return ema
// 	}
// 	log.Info("30min:", ema().Latest())
// 	ticker := time.NewTicker(1 * time.Minute)
// 	defer ticker.Stop()
// 	for {
// 		select {
// 		case <-ticker.C:
// 			ema := ema()
// 			log.Info("30min:", ema.Latest())
// 		}
// 	}
// }

// func updateEMA60() {
// 	// calculate EMA60
// 	btc := ok.NewPair("bch_usd", "this_week", "", "")

// 	btc.GetFutureKlineData("5min")
// 	ticker := time.NewTicker(60 * time.Minute)
// 	defer ticker.Stop()
// }

func doGetFurturePos4Fix(pair *ok.Pair) *ok.FuturePosResp {
	var futurePos *ok.FuturePosResp
	var errNotFound = errors.New("unable to get current holdings")
	err := utils.Do(func(attempt int) (bool, error) {
		var err error
		futurePos, err = pair.GetFuturePos4Fix()
		if len(futurePos.Holdings) < 0 {
			err = errNotFound
		}
		time.Sleep(300 * time.Millisecond)
		return attempt < 10, err // try 10 times
	})

	if err != nil {
		log.WithError(err).Error("unable to get future current future holdings")
		return nil
	}
	return futurePos
}

func doTrade(pair *ok.Pair, price string, amount string, tradeType ok.TradeType, matching bool) bool {
	var errTradeFail = errors.New("failed to trade")
	err := utils.Do(func(attempt int) (bool, error) {
		var err error
		resp := pair.FutureTrade(price, amount, tradeType, matching)
		if !resp.Result {
			err = errTradeFail
		}
		time.Sleep(300 * time.Millisecond)
		return attempt < 10, err // try 5 times
	})
	if err != nil {
		return false
	}
	return true
}

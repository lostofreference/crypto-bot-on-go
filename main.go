package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"gopkg.in/telebot.v3"
)

type Subscription struct {
	ChatID int64         `json:"chatid"`
	Symbol string        `json:"symbol"`
	Time   time.Duration `json:"time"`
}

var (
	subscriptions = make(map[int64]*Subscription)
	subMutex      sync.Mutex
)

const subFile = "subs.json"

func SaveSubs() {
	data, err := json.MarshalIndent(subscriptions, "", "  ")
	if err != nil {
		fmt.Println("error encoding", err)
		return
	}

	err = os.WriteFile(subFile, data, 0644)
	if err != nil {
		fmt.Println("error writing file:", err)
	}
}

func LoadSubs() {
	data, err := os.ReadFile(subFile)
	if err != nil {
		fmt.Println("database not found")
		return
	}
	err = json.Unmarshal(data, &subscriptions)
	if err != nil {
		fmt.Println("error reading database:", err)
	}
}

type PriceRes struct {
	Symbol string `json:"symbol"`
	Price  string `json:"price"`
}

func Fetchprice(symbol string) (float64, error) {
	url := fmt.Sprintf("https://api.binance.com/api/v3/ticker/price?symbol=%s", symbol)

	resp, err := http.Get(url)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	var data PriceRes
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return 0, err
	}

	price, err := strconv.ParseFloat(data.Price, 64)
	if err != nil {
		return 0, err
	}

	return price, nil
}

func main() {
	pref := telebot.Settings{
		Token:  "YOUR-BOT-TOKEN",
		Poller: &telebot.LongPoller{Timeout: 10 * time.Second},
	}

	b, err := telebot.NewBot(pref)
	if err != nil {
		log.Fatal(err)
		return
	}

	LoadSubs()

	for _, sub := range subscriptions {
		minutes := int(sub.Time.Minutes())
		if minutes > 0 {
			go startUserMonitor(b, sub.ChatID, sub.Symbol, minutes)
		}
	}

	b.Handle("/price", func(c telebot.Context) error {
		btc, _ := Fetchprice("BTCUSDT")
		eth, _ := Fetchprice("ETHUSDT")
		msg := fmt.Sprintf("Current rates:\n\nBTC: $%.2f\nETH: $%.2f", btc, eth)
		return c.Send(msg, telebot.ModeMarkdown)
	})

	b.Handle("/coinprice", func(c telebot.Context) error {
		args := c.Args()
		if len(args) < 1 {
			return c.Send("one argument required")
		}
		coin := strings.ToUpper(args[0])
		symbol := coin + "USDT"
		price, err := Fetchprice(symbol)
		if err != nil {
			return c.Send("coin not found: " + coin)
		}
		return c.Send(fmt.Sprintf("Price %s: $%.2f", coin, price))
	})

	b.Handle("/subscribe", func(c telebot.Context) error {
		args := c.Args()
		if len(args) < 2 {
			return c.Send("usage: /subscribe BTC 10")
		}
		coin := strings.ToUpper(args[0])
		symbol := coin + "USDT"
		minutes, err := strconv.Atoi(args[1])
		if err != nil || minutes <= 0 {
			return c.Send("invalid time")
		}
		_, err = Fetchprice(symbol)
		if err != nil {
			return c.Send("coin not found")
		}

		subMutex.Lock()
		subscriptions[c.Sender().ID] = &Subscription{
			ChatID: c.Sender().ID,
			Symbol: symbol,
			Time:   time.Duration(minutes) * time.Minute,
		}
		subMutex.Unlock()
		SaveSubs()

		go startUserMonitor(b, c.Sender().ID, symbol, minutes)
		return c.Send(fmt.Sprintf("subscribed to %s every %d min", coin, minutes))
	})

	b.Handle("/unsubscribe", func(c telebot.Context) error {
		subMutex.Lock()
		delete(subscriptions, c.Sender().ID)
		subMutex.Unlock()
		SaveSubs()
		return c.Send("unsubscribed")
	})

	fmt.Println("bot started")
	b.Start()
}

func startUserMonitor(b *telebot.Bot, chatID int64, symbol string, minutes int) {
	ticker := time.NewTicker(time.Duration(minutes) * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		subMutex.Lock()
		_, exists := subscriptions[chatID]
		subMutex.Unlock()

		if !exists {
			return
		}

		price, err := Fetchprice(symbol)
		if err == nil {
			msg := fmt.Sprintf("Update: %s is $%.2f", symbol, price)
			b.Send(telebot.ChatID(chatID), msg)
		}
	}
}
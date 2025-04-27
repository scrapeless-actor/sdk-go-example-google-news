package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/scrapeless-ai/scrapeless-actor-sdk-go/scrapeless/browser"
	"github.com/tidwall/gjson"
	"log"
	"regexp"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/chromedp/chromedp"
	"github.com/scrapeless-ai/scrapeless-actor-sdk-go/scrapeless"
	proxyModel "github.com/scrapeless-ai/scrapeless-actor-sdk-go/scrapeless/proxy"
)

type RequestParam struct {
	Q  string `json:"q" url:"q"`
	Gl string `json:"gl" url:"gl"`
	Hl string `json:"hl" url:"hl"`
}

var ()

func main() {
	actor := scrapeless.New(scrapeless.WithProxy(), scrapeless.WithBrowser(), scrapeless.WithStorage())
	defer actor.Close()
	var param = &RequestParam{}
	if err := actor.Input(param); err != nil {
		log.Fatal(err)
	}
	// Get proxy
	proxy, err := actor.Proxy.Proxy(context.TODO(), proxyModel.ProxyActor{
		Country:         "us",
		SessionDuration: 10,
	})
	if err != nil {
		log.Fatal(err)
	}
	doCrawl(actor, param, proxy)
}

func doCrawl(actor *scrapeless.Actor, param *RequestParam, proxy string) {
	browserInfo, err := actor.Browser.Create(context.Background(), browser.Actor{
		Input:        browser.Input{SessionTtl: "180"},
		ProxyCountry: "US",
		ProxyUrl:     proxy,
	})
	if err != nil {
		log.Fatal(err)
	}
	searchUrl := fmt.Sprintf("https://news.google.com/search?q=%s&hl=%s&gl=%s", param.Q, param.Hl, param.Gl)
	doc, err := getHtml(searchUrl, browserInfo.DevtoolsUrl)
	search, err := Search(context.Background(), doc)
	if err != nil {
		log.Fatal(err)
	}
	ok, err := actor.Storage.GetDataset().AddItems(context.Background(), []map[string]any{
		{
			"url":  searchUrl,
			"data": search,
		}})
	if err != nil {
		log.Println(err)
		return
	}
	log.Println("ok:", ok)
}

func getHtml(url string, devToolsUrl string) (string, error) {
	return chromedpScrape(url, devToolsUrl)

}

func chromedpScrape(url string, devtoolsWsURL string) (string, error) {
	var htmlContent string
	allocatorCtx, cancel := chromedp.NewRemoteAllocator(context.Background(), devtoolsWsURL, chromedp.NoModifyURL)
	defer cancel()
	ctx, cancel := chromedp.NewContext(allocatorCtx)
	defer cancel()
	ctx, cancel = context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	err := chromedp.Run(ctx,
		chromedp.Tasks{
			chromedp.Navigate(url),
			chromedp.WaitReady("body"),
			chromedp.OuterHTML("html", &htmlContent),
		},
	)
	if err != nil {
		log.Printf("chromedp err:%v", err)
		return "", err
	}
	return htmlContent, nil
}

type SearchNewsInfo struct {
	Position int    `json:"position"`
	Title    string `json:"title"`
	Stories  []any  `json:"stories"`
}

type SearchInfo struct {
	NewsResults   any `json:"news_results"`
	MenuLinks     any `json:"menu_links"`
	RelatedTopics any `json:"related_topics"`
}

func Search(ctx context.Context, data string) ([]byte, error) {
	var (
		newsResults = make([]any, 0)
	)
	newsInfo := make([]string, 0)

	dc, err := goquery.NewDocumentFromReader(strings.NewReader(data))
	if err != nil {
		log.Println(err)
		return nil, err
	}
	ml := SearchGetMenuLinks(dc)
	rt := SearchGetRelatedTopics(data)
	re := regexp.MustCompile(`data:\["gsrres".*?sideChannel?`)
	matchString := re.FindAllString(data, -1)
	for _, s := range matchString {
		s = strings.Replace(s, "data:", "", -1)
		s = strings.Replace(s, ", sideChannel", "", -1)
		newsInfo = append(newsInfo, s)
	}
	allNews := gjson.Parse(newsInfo[0]).Get("1.0").Array()
	for i, v := range allNews {
		if v.Get("0").IsArray() {
			for _, v := range allNews[i].Array() {
				parseData := ParseData(i+1, v.Array())
				parseData.StoryToken = ""
				newsResults = append(newsResults, parseData)
				break
			}
			continue
		}
		title := v.Get("1.0").String()
		newsIno := v.Get("1.2").Array()
		searchNewsInfo := SearchNewsInfo{
			Position: i + 1,
			Title:    title,
			Stories:  nil,
		}
		for newsIndex, v := range newsIno {
			parseData := ParseData(newsIndex+1, v.Array())
			searchNewsInfo.Stories = append(searchNewsInfo.Stories, *parseData)
		}
		newsResults = append(newsResults, searchNewsInfo)
	}
	searchInfo := SearchInfo{
		NewsResults:   newsResults,
		MenuLinks:     ml,
		RelatedTopics: rt,
	}
	resultBytes, _ := json.Marshal(searchInfo)
	return resultBytes, nil
}

type MenuLink struct {
	Title      string `json:"title"`
	TopicToken string `json:"topic_token"`
}

func SearchGetMenuLinks(dc *goquery.Document) (ml []MenuLink) {
	dc.Find("c-wiz[jsrenderer='xhgKH']").Children().Eq(0).Children().Each(func(i int, selection *goquery.Selection) {
		val, _ := selection.Find("a").Attr("href")
		if strings.Contains(val, "topics") {
			split := strings.Split(val, "/")
			token := strings.Split(split[len(split)-1], "?")[0]
			ml = append(ml, MenuLink{
				Title:      selection.Find("a").Text(),
				TopicToken: token,
			})
		}
	})
	return
}

type RelatedTopics struct {
	TopicToken string `json:"topic_token"`
	Title      string `json:"title"`
	Thumbnail  string `json:"thumbnail,omitempty"`
}

// SearchGetRelatedTopics related_topics
func SearchGetRelatedTopics(data string) (rt []RelatedTopics) {
	var (
		isExists = make(map[string]struct{})
	)
	newsInfo := make([]string, 0)
	re := regexp.MustCompile(`data:\["gsares".*?sideChannel?`)
	matchString := re.FindAllString(data, -1)
	for _, s := range matchString {
		s = strings.Replace(s, "data:", "", -1)
		s = strings.Replace(s, ", sideChannel", "", -1)
		newsInfo = append(newsInfo, s)
	}
	thumbnail := gjson.Parse(newsInfo[0]).Get("1.0.2.17.0.0").String()
	title := gjson.Parse(newsInfo[0]).Get("1.0.2.2").String()
	topic := gjson.Parse(newsInfo[0]).Get("1.0.2.1.1").String()
	rt = append(rt, RelatedTopics{
		TopicToken: topic,
		Title:      title,
		Thumbnail:  thumbnail,
	})
	isExists[title] = struct{}{}
	for _, result := range gjson.Parse(newsInfo[0]).Get("1.1.0").Array() {
		title := result.Get("0").String()
		topic := result.Get("2").String()
		split := strings.Split(topic, "/")
		if len(split) != 0 {
			topic = split[len(split)-1]
		}
		rt = append(rt, RelatedTopics{
			TopicToken: topic,
			Title:      title,
		})
	}
	return
}

type NewsParse struct {
	Position       int    `json:"position"`
	Title          string `json:"title"`
	Source         Source `json:"source"`
	Link           string `json:"link"`
	Thumbnail      string `json:"thumbnail"`
	ThumbnailSmall string `json:"thumbnail_small"`
	StoryToken     string `json:"story_token,omitempty"`
	Date           string `json:"date"`
}
type Source struct {
	Name    string   `json:"name,omitempty"`
	Icon    string   `json:"icon,omitempty"`
	Authors []string `json:"authors,omitempty"`
}

func ParseData(position int, array []gjson.Result) *NewsParse {
	fuInfo := &NewsParse{
		Position: position,
	}
	timestamp := array[4].Array()[0].Int()
	utcTime := time.Unix(timestamp, 0).UTC()
	fuInfo.Date = utcTime.Format("2006-01-02 15:04:05 MST")
	// title
	title := array[2].String()
	fuInfo.Title = title
	//storyToken
	storyTokenInfo := gjson.Parse(array[len(array)-5].String()).Array()
	if len(storyTokenInfo) != 0 {
		fuInfo.StoryToken = storyTokenInfo[len(storyTokenInfo)-1].String()
	}
	// source.authors
	for _, result := range gjson.Parse(array[len(array)-1].String()).Get("0").Array() {
		fuInfo.Source.Authors = append(fuInfo.Source.Authors, result.String())
	}
	// source.name
	sourceName := array[10].Get("2").String()
	fuInfo.Source.Name = sourceName
	// source.icon
	//sourceIcon := array[10].Array()[23].String()
	sourceIcon := array[10].Get("22.0").String()
	fuInfo.Source.Icon = sourceIcon
	//link
	fuInfo.Link = array[38].String()

	//thumbnail
	fuInfo.Thumbnail = array[8].Get("0.13").String()

	//thumbnail_small
	thumbnailSmall := array[8].Get("0.0").String()
	fuInfo.ThumbnailSmall = fmt.Sprintf("%s%s", "https://news.google.com/api", thumbnailSmall)
	return fuInfo
}

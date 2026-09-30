package main

import (
	"AutoSign/geopicker"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/tidwall/gjson"
)

type Payload struct {
	OperationName string     `json:"operationName"`
	Variables     Variables  `json:"variables"`
	Extensions    Extensions `json:"extensions"`
}

type Variables struct {
	Input Input `json:"input"`
}

type Input struct {
	FormID          string         `json:"formId"`
	Geetest4Data    any            `json:"geetest4Data"`
	CaptchaData     any            `json:"captchaData"`
	PrefilledParams string         `json:"prefilledParams"`
	HasPreferential bool           `json:"hasPreferential"`
	ForceSubmit     bool           `json:"forceSubmit"`
	EntryAttributes map[string]any `json:"entryAttributes"` // 动态 key
	FillingDuration int            `json:"fillingDuration"`
	Embedded        bool           `json:"embedded"`
	BackgroundImage bool           `json:"backgroundImage"`
	FormMargin      bool           `json:"formMargin"`
	Internal        bool           `json:"internal"`
	Code            any            `json:"code"`
}

type Field5 struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
	Address   string  `json:"address"`
}

type Extensions struct {
	PersistedQuery PersistedQuery `json:"persistedQuery"`
}

type PersistedQuery struct {
	Version    int    `json:"version"`
	SHA256Hash string `json:"sha256Hash"`
}

func fillOutURI(rawURL string) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		panic(err)
	}

	u.Path = "/graphql" + u.Path
	return u.String(), nil
}

func createField(args []string) map[string]any {
	loc, err := geopicker.Pick(geopicker.Options{})
	//add in v1.1
	if err != nil {
		log.Fatalf("选点失败: %v", err) // ← 一定要打日志
	}

	fmt.Printf("纬度: %.6f  经度: %.6f\n地址: %s\n",
		loc.Latitude, loc.Longitude, loc.Address)
	//
	return map[string]any{
		"field_1": args[0],
		"field_2": args[1],
		"field_3": args[2],
		"field_4": args[3],
		"field_5": Field5{
			Latitude:  loc.Latitude,
			Longitude: loc.Longitude,
			Address:   loc.Address,
		},
	}

}

func process_Resp(result gjson.Result, signed bool, sleepTime int) bool {
	if signed == false {
		//签到的处理
		if !result.IsArray() || len(result.Array()) == 0 {
			fmt.Println("[+]sign in successfully!!!")
			fmt.Println("[+]Begin to wait sign out...")
			time.Sleep(time.Duration(sleepTime) * time.Minute)
			return true
		}
	} else {
		//签退
		if !result.IsArray() || len(result.Array()) == 0 {
			fmt.Println("[+]sign out successful!!!")
			os.Exit(0)
		}
	}
	errMessage := result.Array()[0].Get("message").String()
	errCode := result.Array()[0].Get("code").Int()
	fmt.Println("return error message:", errMessage)
	fmt.Println("return error code:", errCode)
	panic("[-]Failed!")
}

func NewPayload(URL string, element map[string]any) Payload {
	re := regexp.MustCompile(`/f/([^/?#]+)`)
	m := re.FindStringSubmatch(URL)
	if m == nil {
		panic("未匹配")
	}
	params := m[1]
	hash, _ := sign(URL)

	return Payload{
		OperationName: "CreatePublishedFormEntry",
		Variables: Variables{
			Input: Input{
				FormID:          params,
				Geetest4Data:    nil,
				CaptchaData:     nil,
				PrefilledParams: "",
				HasPreferential: false,
				ForceSubmit:     false,
				EntryAttributes: element,
				FillingDuration: 47,
				Embedded:        false,
				BackgroundImage: false,
				FormMargin:      false,
				Internal:        false,
				Code:            nil,
			},
		},
		Extensions: Extensions{
			PersistedQuery: PersistedQuery{
				Version:    1,
				SHA256Hash: hash,
			},
		},
	}
}

func main() {
	var element map[string]any

	args := os.Args[2:]
	sleepTime, _ := strconv.Atoi(os.Args[1])
	exsit := false
	signed := false
	pos := 0

	if v := loadField(); v != nil {
		element = v
		exsit = true
	} else {
		element = createField(args[2:]) //Field是唯一可以永久封装的
	}

	for {
		URL, _ := fillOutURI(args[pos])
		bytes, _ := json.Marshal(NewPayload(args[pos], element))
		//fmt.Println(string(bytes))
		payload := strings.NewReader(string(bytes))
		req, _ := http.NewRequest("POST", URL, payload)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Add("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/153.0.0.0 Safari/537.36")
		req.Header.Add("sec-ch-ua-platform", "Windows")
		req.Header.Add("sec-ch-ua", "\"Google Chrome\";v=\"153\", \"Not_A Brand\";v=\"8\", \"Chromium\";v=\"153\"")

		resp, _ := http.DefaultClient.Do(req)
		defer resp.Body.Close()
		body, _ := ioutil.ReadAll(resp.Body)
		//use gjson lib to parse
		err := gjson.Get(string(body), "data.createPublishedFormEntry.errors")
		signed = process_Resp(err, signed, sleepTime)
		if signed == true && !exsit {
			saveField(element)
		}
		pos++
	}

}

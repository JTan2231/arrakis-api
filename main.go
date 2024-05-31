package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"github.com/jtan2231/arrakis-api/types/discord"
	"golang.org/x/net/html"
	"io"
	"log"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"
)

type RedditAuthToken struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int    `json:"expires_in"`
	Scope       string `json:"scope"`
}

type AuthToken struct {
	Api         string `json:"api"`
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresAt   int    `json:"expires_at"`
}

type RedditPost struct {
	Title string `json:"title"`
	// probably others
}

// generic struct representing a bit of news to be processed for display
type Headline struct {
	Title  string
	Source string
	Body   string
}

const REDDIT_BASE_URL = "https://oauth.reddit.com"
const DISCORD_API_BASE_URL = "https://discord.com/api/v10"

type Integration int

const (
	REDDIT Integration = iota
)

type AuthMap map[Integration]AuthToken

// obviously, don't use this in instances that could leave unfinished artifacts
func errCheck(err error, message string, code int) {
	if err != nil {
		log.Fatal(message, err)
		os.Exit(code)
	}
}

func getRedditAuth(client *http.Client) RedditAuthToken {
	req, err := http.NewRequest("POST", "https://www.reddit.com/api/v1/access_token", nil)
	errCheck(err, "error creating request: ", 1)

	id := os.Getenv("REDDIT_CLIENT_ID")
	secret := os.Getenv("REDDIT_CLIENT_SECRET")

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(id, secret)
	req.Body = io.NopCloser(strings.NewReader("grant_type=client_credentials&scope=read"))
	req.Header.Set("User-Agent", "my-client/0.0.1")
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	errCheck(err, "error sending request: ", 1)

	// get the response body
	body, err := io.ReadAll(resp.Body)
	errCheck(err, "error reading response: ", 1)

	auth := RedditAuthToken{}
	json.Unmarshal(body, &auth)

	log.Println("Refreshed Reddit auth: ", auth)

	return auth
}

func readFile(fileName string) []byte {
	file, err := os.Open(fileName)
	if err != nil {
		log.Println("[WARN] failed to open file: ", err)
		return []byte("{}")
	}

	defer file.Close()

	scanner := bufio.NewScanner(file)
	var contents string
	for scanner.Scan() {
		contents += scanner.Text()
	}

	errCheck(scanner.Err(), "error reading file: ", 1)

	return []byte(contents)
}

func writeFile(fileName string, data []byte) {
	file, err := os.Create(fileName)
	if err != nil {
		log.Fatalf("failed to create file: %s", err)
	}
	defer file.Close()

	_, err = file.Write(data)
	if err != nil {
		log.Fatalf("failed to write to file: %s", err)
	}
}

func getOrRefreshAuth(client *http.Client) map[Integration]AuthToken {
	integrations := []Integration{REDDIT}
	tokens := make(map[Integration]AuthToken, 0)

	contents := readFile("auth.json")

	err := json.Unmarshal(contents, &tokens)
	if err != nil {
		log.Println("[WARN] error reading auth.json: ", err)
		log.Println("[WARN] refreshing all tokens")
		log.Println(contents)
	}

	for _, integration := range integrations {
		token, ok := tokens[integration]
		if !ok || token.ExpiresAt < int(time.Now().Unix()) {
			switch integration {
			case REDDIT:
				log.Println("Refreshing Reddit token")
				redditAuth := getRedditAuth(client)
				refreshedToken := AuthToken{
					Api:         "reddit",
					AccessToken: redditAuth.AccessToken,
					TokenType:   redditAuth.TokenType,
					ExpiresAt:   int(time.Now().Unix()) + redditAuth.ExpiresIn,
				}

				tokens[integration] = refreshedToken
			}
		}
	}

	contents, err = json.Marshal(tokens)
	errCheck(err, "error marshalling tokens: ", 1)

	writeFile("auth.json", contents)

	return tokens
}

func readRedditResponse(jsonData []byte) []RedditPost {
	posts := make([]RedditPost, 0)
	var raw map[string]json.RawMessage
	err := json.Unmarshal(jsonData, &raw)
	errCheck(err, "error unmarshalling response: ", 1)

	var data map[string]json.RawMessage
	err = json.Unmarshal(raw["data"], &data)
	errCheck(err, "error unmarshalling data: ", 1)

	var children []json.RawMessage
	err = json.Unmarshal(data["children"], &children)
	errCheck(err, "error unmarshalling children: ", 1)

	for _, child := range children {
		var childData map[string]json.RawMessage
		err = json.Unmarshal(child, &childData)
		errCheck(err, "error unmarshalling child data: ", 1)

		var post RedditPost
		err = json.Unmarshal(childData["data"], &post)
		errCheck(err, "error unmarshalling post: ", 1)
		posts = append(posts, post)
	}

	return posts
}

func getRedditHeadlines(client *http.Client, auth map[Integration]AuthToken, subreddits []string) []Headline {
	headlines := make([]Headline, 0)

	for _, subreddit := range subreddits {
		log.Println("[INFO] getting headlines for subreddit: ", subreddit)
		req, err := http.NewRequest("GET", REDDIT_BASE_URL+"/r/"+subreddit+"/hot?limit=20&raw_json=1", nil)
		errCheck(err, "error creating request: ", 1)

		token := auth[REDDIT]
		req.Header.Set("Authorization", "bearer "+token.AccessToken)
		req.Header.Set("User-Agent", "arrakis-client/0.0.1")

		resp, err := client.Do(req)
		errCheck(err, "error sending request: ", 1)

		body, err := io.ReadAll(resp.Body)
		errCheck(err, "error reading response: ", 1)

		posts := readRedditResponse(body)

		for _, posts := range posts {
			headline := Headline{
				Title:  posts.Title,
				Source: "www.reddit.com/r/" + subreddit,
			}

			headlines = append(headlines, headline)
		}
	}

	log.Println("[INFO] reddit headline count: ", len(headlines))

	return headlines
}

func findTitleSpans(n *html.Node, headlines *[]Headline) {
	class := "titleline"
	if n.Type == html.ElementNode && n.Data == "span" {
		for _, attr := range n.Attr {
			if attr.Key == "class" && attr.Val == class {
				for c := n.FirstChild; c != nil; c = c.NextSibling {
					// the actual title is inside the <a> tag
					if c.Type == html.ElementNode && c.Data == "a" {
						headline := Headline{
							Title:  c.FirstChild.Data,
							Source: "news.ycombinator.com",
						}

						*headlines = append(*headlines, headline)
					}
				}
			}
		}
	}

	for c := n.FirstChild; c != nil; c = c.NextSibling {
		findTitleSpans(c, headlines)
	}
}

func getHackernewsHeadlines(client *http.Client, pages int) []Headline {
	headlines := make([]Headline, 0)

	for p := 1; p <= pages; p++ {
		log.Println("[INFO] getting hackernews headlines for page: ", p)
		req, err := http.NewRequest("GET", "https://news.ycombinator.com/?p="+fmt.Sprint(p), nil)
		errCheck(err, "error creating request: ", 1)

		resp, err := client.Do(req)
		errCheck(err, "error sending request: ", 1)

		doc, err := html.Parse(resp.Body)
		errCheck(err, "error parsing response: ", 1)

		findTitleSpans(doc, &headlines)
	}

	log.Println("[INFO] hackernews headline count: ", len(headlines))

	return headlines
}

func get4ChanHeadlines(client *http.Client, board string) []Headline {
	headlines := make([]Headline, 0)

	req, err := http.NewRequest("GET", "https://boards.4chan.org/"+board+"/catalog", nil)
	errCheck(err, "error creating request: ", 1)

	resp, err := client.Do(req)
	errCheck(err, "error sending request: ", 1)

	// regex for finding thread ids in the response body
	regex := regexp.MustCompile(`"teaser":"(.*?)"`)
	body, err := io.ReadAll(resp.Body)
	errCheck(err, "error reading response: ", 1)

	threadNumbers := regex.FindAllString(string(body), -1)
	for _, number := range threadNumbers {
		headlines = append(headlines, Headline{
			Title:  strings.Trim(number, "\"teaser\":\""),
			Source: "boards.4chan.org/" + board,
		})
	}

	log.Println("[INFO] /", board, "/ headline count: ", len(headlines))

	return headlines
}

func gptHeadlinePrompt(client *http.Client, headlines []Headline) []string {
	sourceMap := make(map[string][]Headline)
	for _, headline := range headlines {
		sourceMap[headline.Source] = append(sourceMap[headline.Source], headline)
	}

	req, err := http.NewRequest("POST", "https://api.openai.com/v1/chat/completions", nil)
	errCheck(err, "error creating request: ", 1)

	req.Header.Set("Authorization", "Bearer "+os.Getenv("OPENAI_API_KEY"))
	req.Header.Set("Content-Type", "application/json")

	var prompt string
	for source, headlines := range sourceMap {
		prompt += "- " + source + "\n"
		for _, headline := range headlines {
			prompt += "  - " + headline.Title + "\n"
		}
	}

	prompt += "\n\n"

	log.Println("[INFO] Prompt size: ", len(prompt))

	reqBody := map[string]interface{}{
		"model":       "gpt-4o",
		"temperature": 1,
		"messages": []map[string]string{
			{
				"role":    "system",
				"content": "You are a news aggregator bot. Given a list of sources, and a list of posts from each source, provide a nuanced summary of what's being posted. Be thorough, but be careful! The messages can't be too long (2000 character limit!). Details matter, no matter how noisy/inappropriate (don't forget 4chan!). Be specific! Focus on _all_ the topics being talked about, not the fact that the chatter exists. The user already knows what you're being given--there's no need to restate or provide context. Do not segregate, do not organize. Write as if you are speaking with a friend on what you've seen.",
			},
			{
				"role":    "user",
				"content": prompt,
			},
		},
	}

	body, err := json.Marshal(reqBody)
	errCheck(err, "error marshalling request body: ", 1)

	req.Body = io.NopCloser(strings.NewReader(string(body)))

	resp, err := client.Do(req)
	errCheck(err, "error sending request: ", 1)

	body, err = io.ReadAll(resp.Body)
	errCheck(err, "error reading response: ", 1)

	var response map[string]interface{}
	err = json.Unmarshal(body, &response)
	errCheck(err, "error unmarshalling response: ", 1)

	choices := response["choices"].([]interface{})
	message := choices[0].(map[string]interface{})["message"].(map[string]interface{})["content"].(string)

	log.Println("[INFO] GPT response: ", message)

	splits := make([]string, 0)

	base := 2000
	increment := base
	for len(message) > 0 {
		increment = min(base, len(message))
		splits = append(splits, message[0:increment])
		message = message[increment:]
	}

	log.Println("[INFO] split count: ", len(splits))

	return splits
}

func getHeadlinePrompt() []string {
	client := &http.Client{}

	auth := getOrRefreshAuth(client)

	headlines := make([]Headline, 0)
	headlines = append(headlines, getHackernewsHeadlines(client, 5)...)
	headlines = append(headlines, getRedditHeadlines(client, auth, []string{"wallstreetbets", "investmentclub", "stockmarkets", "investing", "cryptocurrency", "cscareerquestions", "worldnews", "stocks"})...)
	headlines = append(headlines, get4ChanHeadlines(client, "biz")...)
	headlines = append(headlines, get4ChanHeadlines(client, "g")...)

	return gptHeadlinePrompt(client, headlines)
}

func sendDiscordRequest(client *http.Client, endpoint string, method string) ([]byte, error) {
	req, err := http.NewRequest(method, DISCORD_API_BASE_URL+endpoint, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "Bot "+os.Getenv("ARRAKIS_TERMINAL_TOKEN"))

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}

	body, err := io.ReadAll(resp.Body)

	return body, err
}

func sendHeadlinePrompt() {
	log.Println("[INFO] sending headline prompt")

	client := &http.Client{}

	serverIDs := make([]string, 0)

	body, err := sendDiscordRequest(client, "/users/@me/guilds", "GET")
	errCheck(err, "error getting bot guilds: ", 1)

	var servers []discord.Guild
	log.Println("[INFO] body: ", string(body))
	err = json.Unmarshal(body, &servers)
	errCheck(err, "error unmarshalling discord servers: ", 1)

	for _, server := range servers {
		serverIDs = append(serverIDs, server.ID)
	}

	log.Println("[INFO] server IDs: ", serverIDs)

	validChannelNames := [3]string{"arrakis-terminal", "money-talk", "arrakeen"}
	isValidChannel := func(name string) bool {
		for _, validName := range validChannelNames {
			if name == validName {
				return true
			}
		}

		return false
	}

	var channels []discord.Channel
	for _, serverID := range serverIDs {
		body, err := sendDiscordRequest(client, "/guilds/"+serverID+"/channels", "GET")
		errCheck(err, "error getting discord channels for guild "+serverID+": ", 1)

		var serverChannels []discord.Channel
		err = json.Unmarshal(body, &serverChannels)
		errCheck(err, "error unmarshalling discord channels: ", 1)

		for _, channel := range serverChannels {
			if channel.Type == 0 && isValidChannel(channel.Name) {
				channels = append(channels, channel)
			}
		}
	}

	log.Println("[INFO] sending updates to channels: ", channels)

	prompt := getHeadlinePrompt()
	for _, channel := range channels {
		if channel.Type == 0 {
			for i, split := range prompt {
				reqBody := map[string]interface{}{
					"content": split,
				}

				body, err := json.Marshal(reqBody)
				errCheck(err, "error marshalling request body: ", 1)

				req, err := http.NewRequest("POST", DISCORD_API_BASE_URL+"/channels/"+channel.ID+"/messages", nil)
				errCheck(err, "error creating request: ", 1)

				req.Header.Set("Authorization", "Bot "+os.Getenv("ARRAKIS_TERMINAL_TOKEN"))
				req.Header.Set("Content-Type", "application/json")
				req.Body = io.NopCloser(strings.NewReader(string(body)))

				log.Println("[INFO] sending split ", i, " of ", len(prompt))
				response, err := client.Do(req)
				log.Println("[INFO] response: ", response)
				errCheck(err, "error sending request: ", 1)

				time.Sleep(500 * time.Millisecond)
			}
		}
	}
}

func main() {
	// set log output to stdout
	log.SetOutput(os.Stdout)

	sendHeadlinePrompt()
}

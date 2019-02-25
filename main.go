package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"

	"github.com/Shopify/ejson"
	"github.com/jrudio/go-plex-client"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

type unogsResponse struct {
	Count string              `json:"COUNT"`
	Items []map[string]string `json:"ITEMS"`
}

type netflixLookup struct {
	Result netflixLookupResult `json:"RESULT"`
}

type netflixLookupResult struct {
	Country []netflixCountry `json:"country"`
}

type netflixCountry struct {
	Code string `json:"ccode"`
}

func main() {
	host := flag.String("plex-host", "localhost", "the hostname of the plex server")

	logger := logrus.New()
	logger.Formatter = &logrus.TextFormatter{}
	logger.Out = os.Stdout

	secrets, err := getSecrets()
	if err != nil {
		logger.WithField("error", err).Fatal("getting secrets")
		os.Exit(1)
	}

	plexConn, err := plex.New(fmt.Sprintf("http://%s:32400", *host), secrets["PLEX_TOKEN"])
	if err != nil {
		logger.WithField("error", err).Fatal("creating plex client")
		os.Exit(1)
	}

	sections, err := plexConn.GetLibraries()
	if err != nil {
		logger.WithField("error", err).Fatal("getting libraries")
		os.Exit(1)
	}

	for _, dir := range sections.MediaContainer.Directory {
		logger.WithField("section", dir.Title).Info("searching section")
		results, err := plexConn.GetLibraryContent(dir.Key, "")
		if err != nil {
			logger.WithField("error", err).WithField("library", dir.Key).Fatal("getting library")
		}

		for _, metadata := range results.MediaContainer.Metadata {
			found, err := findOnNetflix(metadata.Title, metadata.Year, secrets["RAPID_API_KEY"])
			if err != nil {
				logger.WithField("error", err).WithField("title", metadata.Title).Fatal("finding on Netflix")
			}

			if found {
				logger.WithField("title", metadata.Title).Info("found on netflix")
			}
		}
	}
}

func findOnNetflix(title string, year int, apiKey string) (bool, error) {
	netflixID, err := findNetflixID(title, year, apiKey)
	if err != nil {
		return false, errors.Wrap(err, "finding Netflix ID")
	}

	if netflixID == "" {
		return false, nil
	}

	return findOnNetflixUSA(netflixID, apiKey)
}

func findNetflixID(title string, year int, apiKey string) (string, error) {
	r, err := regexp.Compile(`\(\d{4}\)$`)
	if err != nil {
		return "", errors.Wrap(err, "compiling regexp")
	}
	title = r.ReplaceAllString(title, "")
	title = strings.Replace(title, "'", "", -1)
	title = strings.TrimSpace(title)

	bytes, err := callUnogs(
		fmt.Sprintf(
			"https://unogs-unogs-v1.p.rapidapi.com/aaapi.cgi?q=%s-!%d,%d-!0,5-!0,10-!0-!Any-!Any-!Any-!gt100-!{downloadable}&t=ns&cl=all&st=adv&ob=Relevance&p=1&sa=and",
			url.QueryEscape(title),
			year,
			year,
		),
		apiKey,
	)
	if err != nil {
		return "", err
	}
	var result unogsResponse
	err = json.Unmarshal(bytes, &result)
	if err != nil {
		return "", errors.Wrapf(err, "unmarshaling netflix API response: %v", string(bytes))
	}

	for _, item := range result.Items {
		if item["title"] == title {
			return item["netflixid"], nil
		}
	}

	return "", nil
}

func findOnNetflixUSA(id, apiKey string) (bool, error) {
	bytes, err := callUnogs(fmt.Sprintf("https://unogs-unogs-v1.p.rapidapi.com/aaapi.cgi?t=loadvideo&q=%s", id), apiKey)
	if err != nil {
		return false, err
	}
	var lookup netflixLookup
	err = json.Unmarshal(bytes, &lookup)
	if err != nil {
		return false, errors.Wrapf(err, "unmarshaling netflix API response: %v", string(bytes))
	}

	for _, country := range lookup.Result.Country {
		if country.Code == "us" {
			return true, nil
		}
	}

	return false, nil
}

func getSecrets() (map[string]string, error) {
	bytes, err := ejson.DecryptFile("secrets.json", "/opt/ejson/keys", "")
	if err != nil {
		return nil, errors.Wrap(err, "reading secrets.json")
	}
	secrets := map[string]string{}
	err = json.Unmarshal(bytes, &secrets)
	if err != nil {
		return nil, errors.Wrap(err, "unmarshaling secrets")
	}
	return secrets, nil
}

func callUnogs(url, apiKey string) ([]byte, error) {
	httpClient := http.Client{}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, errors.Wrap(err, "creating request")
	}
	req.Header.Add("X-RapidAPI-Key", apiKey)
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}

	bytes, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, errors.Wrap(err, "reading uNoGS body")
	}

	return bytes, nil
}

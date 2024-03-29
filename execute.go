package bird_region_rosters

import (
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/gbdubs/amass"
	"github.com/gbdubs/attributions"
)

type avibaseEntry struct {
	EnglishName string
	LatinName   string
	AvibaseId   string
}

const rootUrl = "http://avibase.bsc-eoc.org/"
const checklistUrl = rootUrl + "checklist.jsp"

func (input *Input) Execute() (output *Output, err error) {
	input.VLog("Starting avibase downloader...\n")
	output = &Output{}
	if !input.ForceReload {
		output, err = input.readMemoized()
		if err == nil {
			input.VLog("Found Memoized - returning.\n")
			return
		}
	}
	avibaseEntries := make([]avibaseEntry, 0)
	for _, regionCode := range input.RegionCodes {
		e, a, er := executeForRegion(regionCode, input.IncludeRare)
		if er != nil {
			err = er
			return
		}
		avibaseEntries = append(avibaseEntries, e...)
		output.Attributions = append(output.Attributions, a)
	}
	input.VLog("Found %d avibase entries. Now looking for synonyms...\n", len(avibaseEntries))
	synonymRequests := make([]*amass.GetRequest, 0)
	for _, avibaseEntry := range avibaseEntries {
		synonymRequests = append(synonymRequests, avibaseEntry.getSynonymsRequests()...)
	}
	amasser := amass.Amasser{
		TotalMaxConcurrentRequests: 10,
		Verbose:                    input.VIndent(),
		AllowedErrorProportion:     .01,
	}
	synonymResponses, err := amasser.GetAll(synonymRequests)
	if err != nil {
		return output, fmt.Errorf("Error during synonym lookup: %v", err)
	}
	output.Entries = processGetResponses(synonymResponses)

	err = input.writeMemoized(output)
	if err != nil {
		err = fmt.Errorf("memoization failed: %v", err)
	}
	input.VLog("Avibase downloader done.\n")
	return
}

// TODO move this over to amasser as well.
func executeForRegion(regionCode string, includeRare bool) (entries []avibaseEntry, attribution attributions.Attribution, err error) {
	req, err := http.NewRequest("GET", checklistUrl, nil)
	if err != nil {
		return
	}
	q := req.URL.Query()
	q.Add("region", regionCode)
	req.URL.RawQuery = q.Encode()
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		err = fmt.Errorf("request failed: %v", err)
		return
	}
	if resp.StatusCode != 200 {
		err = fmt.Errorf("request failed: %d %s", resp.StatusCode, resp.Status)
		return
	}
	defer resp.Body.Close()
	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		err = fmt.Errorf("parse document failed: %v", err)
	}
	doc.Find("tr").Each(func(i int, s *goquery.Selection) {
		if s.Find("td").Length() == 3 {
			entry := avibaseEntry{}
			isRare := false
			s.Find("td").Each(func(j int, ss *goquery.Selection) {
				if j == 0 {
					entry.EnglishName = ss.Text()
				} else if j == 1 {
					entry.LatinName = ss.Find("i").Text()
					partialUrl, _ := ss.Find("a").Attr("href")
					entry.AvibaseId = regexp.MustCompile("avibaseid=([0-9A-F]+)").FindStringSubmatch(partialUrl)[1]
				} else if j == 2 {
					if strings.Contains(ss.Text(), "Rare") {
						isRare = true
					}
				}
			})
			if !isRare || includeRare {
				entries = append(entries, entry)
			}
		}
	})
	attribution = attributions.Attribution{
		OriginUrl:           req.URL.String(),
		CollectedAt:         time.Now(),
		OriginalTitle:       doc.Find("title").Text(),
		Author:              "Avibase - Denis LePage",
		AuthorUrl:           rootUrl,
		ScrapingMethodology: "github.com/gbdubs/avibase_downloader",
		Context:             []string{"Scraped the Avibase Website to list the set of birds that can be found in a given region."},
	}
	return
}

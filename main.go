package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/xrash/smetrics"
)

func main() {
	searcher := Searcher{}
	err := searcher.Load("completeworks.txt")
	if err != nil {
		log.Fatal(err)
	}

	fs := http.FileServer(http.Dir("./static"))
	http.Handle("/", fs)

	http.HandleFunc("/search", handleSearch(searcher))

	port := os.Getenv("PORT")
	if port == "" {
		port = "3001"
	}

	fmt.Printf("Listening on port %s...\n", port)
	err = http.ListenAndServe(fmt.Sprintf(":%s", port), nil)
	if err != nil {
		log.Fatal(err)
	}
}

// SearchResults is a map that stores a map of
// strings with a string array used to output results
type SearchResults map[string][]string

// Searcher handles loading our corpus and searching it
type Searcher struct {
	CompleteWorks string
	// Words represent words in the body of text, including the positions
	// where they're found so we can easily show the results later
	Words map[string][]int
}

func handleSearch(searcher Searcher) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		query, ok := r.URL.Query()["q"]
		if !ok || len(query[0]) < 1 {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte("missing search query in URL params"))
			return
		}
		results := searcher.Search(query[0])
		buf := &bytes.Buffer{}
		enc := json.NewEncoder(buf)
		err := enc.Encode(results)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte("encoding failure"))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(buf.Bytes())
	}
}

// cleanWord removes punctuation, whitespace and other characters from words
// as well as converts it to lowercase
func cleanWord(s string) string {
	// Clean up any punctuation for better search
	punctuation := []string{",", ".", "?", "!", ";", "-", "[", "]", "_", "'", "`"}
	for _, p := range punctuation {
		s = strings.ReplaceAll(s, p, "")
	}

	s = strings.TrimSpace(s)
	s = strings.ToLower(s)

	return s
}

// Load takes a filename and indexes it's contents
func (s *Searcher) Load(filename string) error {
	dat, err := ioutil.ReadFile(filename)
	if err != nil {
		return fmt.Errorf("Load: %w", err)
	}
	s.CompleteWorks = string(dat)
	s.Words = map[string][]int{}

	// Go through all the words and split them manually,
	// so we can easily keep track of our index (position)
	var sb strings.Builder
	for i, r := range s.CompleteWorks {
		if (r == ' ' || r == '\n') && sb.Len() > 1 {
			word := cleanWord(sb.String())
			sb.Reset()

			s.Words[word] = append(s.Words[word], i-utf8.RuneCountInString(word))
			continue
		}

		sb.WriteRune(r)
	}

	return nil
}

// Search takes an input query and returns results from
// our corpus based on the words contained
func (s *Searcher) Search(query string) SearchResults {
	start := time.Now()
	results := SearchResults{}
	results["replaced"] = []string{}

	// We shouldn't search for letters
	if len(query) < 2 {
		t := time.Now()
		elapsed := t.Sub(start)
		results["time"] = []string{fmt.Sprintf("%v", elapsed)}
		return results
	}

	// Find all the valid, unique queries (words) and clean them up
	queryMap := map[string]bool{}
	rawQueries := []string{}
	for _, query := range strings.Split(query, " ") {
		query = cleanWord(query)
		if len(query) > 1 {
			if !queryMap[query] {
				rawQueries = append(rawQueries, query)
			}
			queryMap[query] = true
		}
	}

	// Account for spelling mistakes by looking at similar words
	missingQueries := []string{}
	replacedQueries := map[string]string{}
	bestSimilarityScore := map[string]float64{}
	queries := []string{}
	for _, query := range rawQueries {
		if len(s.Words[query]) == 0 {
			missingQueries = append(missingQueries, query)

			for word := range s.Words {
				// TODO: Similarity could be stored as a score, and then each cluster's
				// score computed according to the sum. This would bubble up
				// exact matches and more similar words and allow better results
				similarity := smetrics.JaroWinkler(query, word, 0.5, 3)
				if bestSimilarityScore[query] < similarity && similarity > 0.85 {
					bestSimilarityScore[query] = similarity
					replacedQueries[query] = word
				}
			}

		} else {
			queries = append(queries, query)
		}
	}

	// Do we still have more missing queries than ones we replaced?
	// If so, we got no results for this search
	if len(missingQueries) > len(replacedQueries) {
		t := time.Now()
		elapsed := t.Sub(start)
		results["time"] = []string{fmt.Sprintf("%v", elapsed)}
		return results
	}

	// Make the replaced queries the new search
	for original, query := range replacedQueries {
		// TODO: Output JSON structure needs to be better defined and these need to be a KV pair
		results["replaced"] = append(results["replaced"], []string{original, query}...)
		queries = append(queries, query)
	}

	// Get a map and list of all the positions our words appear in
	positions := map[int]string{}
	positionList := []int{}
	for _, query = range queries {
		for i := 0; i < len(s.Words[query]); i++ {
			positions[s.Words[query][i]] = query
			positionList = append(positionList, s.Words[query][i])
		}
	}

	// No results?
	if len(positionList) < 1 {
		t := time.Now()
		elapsed := t.Sub(start)
		results["time"] = []string{fmt.Sprintf("%v", elapsed)}
		return results
	}

	// Sort our positions to build clusters
	sort.Ints(positionList)

	// Max distance in runes between one word and the next in a cluster
	maxDistance := 50

	// Build the clusters
	clusters := [][]int{}
	cluster := []int{}
	for i := 0; i < len(positionList)-1; i++ {
		cluster = append(cluster, positionList[i])
		if positionList[i+1]-positionList[i]+utf8.RuneCountInString(positions[positionList[i]]) > maxDistance {
			if len(cluster) > 0 {
				clusters = append(clusters, cluster)
				cluster = []int{}
			}

			continue
		}

	}
	clusters = append(clusters, cluster)

	// Validate the clusters making sure each one has all our search words
	validClusters := [][]int{}
	for _, c := range clusters {
		// If this cluster contains less words than our
		// search query, it can't be a good match
		if len(c) < len(queries) {
			continue
		}

		// A cluster of a single term is always valid for single-term searches
		if len(c) == 1 && len(queries) == 1 {
			validClusters = append(validClusters, c)
			continue
		}

		// Which unique terms does this cluster contain?
		terms := map[string]bool{}
		for i := 0; i < len(c); i++ {
			terms[positions[c[i]]] = true
		}
		if len(terms) == len(queries) {
			validClusters = append(validClusters, c)
		}
	}

	// Return snippet results for all our valid clusters
	snippetSurround := 50 // chars of surrounding text to include
	for _, c := range validClusters {
		if len(c) == 0 {
			continue
		}

		snippet := s.CompleteWorks[c[0]-snippetSurround : c[len(c)-1]+snippetSurround]

		// We don't want to break the snippet mid words, so start working inwards till the first space
		for i := range snippet {
			if snippet[i] == ' ' {
				snippet = snippet[i:]
				break
			}
		}
		lastSpace := -1
		for i := range snippet {
			if snippet[i] == ' ' {
				lastSpace = i
			}
		}
		if lastSpace > -1 {
			snippet = snippet[0:lastSpace]
		}

		// Bold all the matches so it's visually easier for the user
		// to recognize his search in the results
		for _, query := range queries {
			searchRegex := regexp.MustCompile("(?i)(" + query + ")")
			snippet = searchRegex.ReplaceAllString(snippet, "<b>$1</b>")
		}

		results["results"] = append(results["results"], snippet)
	}

	t := time.Now()
	elapsed := t.Sub(start)
	results["time"] = []string{fmt.Sprintf("%v", elapsed)}
	return results
}

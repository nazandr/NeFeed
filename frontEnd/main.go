package main

import (
	"encoding/json"
	"fmt"
	"html/template"
	"io/ioutil"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"gopkg.in/mgo.v2/bson"

	"github.com/gorilla/mux"
)

type Tag struct {
	Name  string
	Value string
}
type TagJSON struct {
	Name string `json:"Name"`
}
type Tags struct {
	Tags []Tag
}

type Article struct {
	Id         bson.ObjectId   `bson:"_id,omitempty"`
	Title      string          `bson:"title"`
	Link       string          `bson:"link"`
	Source     string          `bson:"source"`
	Text       string          `bson:"text"`
	Duplicates []bson.ObjectId `bson:"duplicates"`
	Timestamp  time.Time       `bson:"timestamp"`
}

var (
	tags []TagJSON
	T    Tags
)

func main() {
	file, err := ioutil.ReadFile("./tags.json")
	if err != nil {
		log.Fatal(err)
	}
	json.Unmarshal(file, &tags)
	for _, i := range tags {
		T.Tags = append(T.Tags, Tag{i.Name, i.Name})
	}
	router := mux.NewRouter()
	router.HandleFunc("/", mainPage)
	router.HandleFunc("/auth", auth)
	router.HandleFunc("/login", login)
	router.HandleFunc("/registration", reg)
	router.HandleFunc("/feed/{page:[0-9]+}", feed)
	router.HandleFunc("/ratelike/{id}", like)
	router.HandleFunc("/ratedislike/{id}", dislike)
	http.ListenAndServe(":9000", router)
}
func mainPage(w http.ResponseWriter, req *http.Request) {
	t := template.Must(template.ParseFiles(
		"./templates/main.html",
		"./templates/header.html",
		"./templates/footer.html",
	))
	err := t.Execute(w, nil)
	if err != nil {
		log.Println(err)
	}
}

func like(w http.ResponseWriter, req *http.Request) {
	token, err := req.Cookie("auth")
	if err != nil {
		log.Println(err)
		http.Redirect(w, req, "/", 302)
	}
	id := mux.Vars(req)["id"]
	url := "http://server:12345/ratelike/" + id
	r, err := http.NewRequest("POST", url, nil)
	if err != nil {
		log.Println(err)
		http.Redirect(w, req, "/", 302)
		return
	}
	c := &http.Client{}
	r.Header.Add("auth", token.Value)
	_, err = c.Do(r)
	if err != nil {
		log.Printf("http.Do() error: %v\n", err)
		http.Redirect(w, req, "/", 302)
		return
	}
	http.Redirect(w, req, "/feed/0", 302)
}

func dislike(w http.ResponseWriter, req *http.Request) {
	token, err := req.Cookie("auth")
	if err != nil {
		log.Println(err)
		http.Redirect(w, req, "/", 302)
	}
	id := mux.Vars(req)["id"]
	url := "http://server:12345/ratedislike/" + id
	r, err := http.NewRequest("POST", url, nil)
	if err != nil {
		log.Println(err)
		http.Redirect(w, req, "/", 302)
		return
	}
	c := &http.Client{}
	r.Header.Add("auth", token.Value)
	_, err = c.Do(r)
	if err != nil {
		log.Printf("http.Do() error: %v\n", err)
		http.Redirect(w, req, "/", 302)
		return
	}
	http.Redirect(w, req, "/feed/0", 302)
}

func feed(w http.ResponseWriter, req *http.Request) {
	token, err := req.Cookie("auth")
	if err != nil {
		log.Println(err)
		http.Redirect(w, req, "/", 302)
	}
	page := mux.Vars(req)

	if token.Value == "" {
		http.Redirect(w, req, "/", 302)
	} else {
		url := "http://server:12345/feed/" + page["page"]
		r, err := http.NewRequest("GET", url, nil)
		if err != nil {
			log.Println(err)
			http.Redirect(w, req, "/", 302)
			return
		}
		c := &http.Client{}
		r.Header.Add("auth", token.Value)
		resp, err := c.Do(r)
		if err != nil {
			log.Printf("http.Do() error: %v\n", err)
			http.Redirect(w, req, "/", 302)
			return
		}
		ar, _ := ioutil.ReadAll(resp.Body)

		var articles []Article
		err = json.Unmarshal(ar, &articles)
		if err != nil {
			log.Printf("json unmarshal %v\n", err)
			http.Redirect(w, req, "/", 302)
			return
		}
		t := template.Must(template.ParseFiles(
			"./templates/feed.html",
			"./templates/header.html",
			"./templates/footer.html",
		))
		nPages, err := strconv.Atoi(strings.TrimSpace(resp.Header.Get("npage")))
		if err != nil {
			log.Println("str to int err: ", err)
			http.Redirect(w, req, "/", 302)
		}
		var pages []int
		for i := 0; i <= nPages; i++ {
			pages = append(pages, i)
		}
		a := struct {
			Art   []Article
			Pages []int
		}{
			articles,
			pages,
		}
		err = t.Execute(w, a)
		if err != nil {
			log.Printf("template %v\n", err)
			http.Redirect(w, req, "/", 302)
			return
		}
	}
}

func auth(w http.ResponseWriter, req *http.Request) {
	t := template.Must(template.ParseFiles(
		"./templates/auth.html",
		"./templates/header.html",
		"./templates/footer.html",
	))
	h := struct {
		Title string
		Tags  []Tag
	}{
		Title: "Sing in",
		Tags:  T.Tags,
	}
	err := t.Execute(w, h)
	if err != nil {
		log.Println(err)
	}
}

func reg(w http.ResponseWriter, req *http.Request) {
	req.ParseForm()
	r, err := http.NewRequest("PUT", "http://server:12345/auth", strings.NewReader(req.Form.Encode()))
	if err != nil {
		log.Println(err)
	}
	c := &http.Client{}
	r.Header.Add("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.Do(r)
	if err != nil {
		fmt.Printf("http.Do() error: %v\n", err)
		return
	}
	log.Println(resp.StatusCode)
	log.Println(resp.Header.Get("auth"))
	cookie := http.Cookie{Name: "auth", Value: resp.Header.Get("auth")}
	http.SetCookie(w, &cookie)
	defer resp.Body.Close()
	http.Redirect(w, req, "/feed/0", 302)
}

func login(w http.ResponseWriter, req *http.Request) {
	req.ParseForm()
	r, err := http.NewRequest("POST", "http://server:12345/auth", strings.NewReader(req.Form.Encode()))

	if err != nil {
		log.Println(err)
	}
	c := &http.Client{}
	r.Header.Add("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.Do(r)
	if err != nil {
		fmt.Printf("http.Do() error: %v\n", err)
		return
	}
	ar, _ := ioutil.ReadAll(resp.Body)
	log.Println(string(ar))
	cookie := http.Cookie{Name: "auth", Value: resp.Header.Get("auth")}
	http.SetCookie(w, &cookie)
	defer resp.Body.Close()
	http.Redirect(w, req, "/feed/0", 302)
}

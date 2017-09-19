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

type FeedArticle struct {
	Id        bson.ObjectId `bson:"_id,omitempty"`
	Checked   bool
	Title     string    `bson:"title"`
	Link      string    `bson:"link"`
	Source    string    `bson:"source"`
	Text      string    `bson:"text"`
	Timestamp time.Time `bson:"timestamp"`
}

type UserPublic struct {
	Id          bson.ObjectId   `bson:"_id,omitempty"`
	Email       string          `bson:"email"`
	Tags        []string        `bson:"tags"`
	Feed        []bson.ObjectId `bson:"feed"`
	LikeNews    []bson.ObjectId `bson:"likeNews"`
	DislikeNews []bson.ObjectId `bson:"dislikeNews"`
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
	router.HandleFunc("/logout", logout)
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
	token, err := req.Cookie("auth")
	var a bool
	if (err != nil) || (token.Value == "") {
		a = false
	} else {
		a = true
	}
	data := struct {
		Title string
		Auth  bool
	}{
		"NeFeed",
		a,
	}
	err = t.Execute(w, data)
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
		http.Redirect(w, req, "/auth", 302)
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
		nPages, err := strconv.Atoi(resp.Header.Get("npage"))
		if err != nil {
			log.Println("str to int err: ", err)
			http.Redirect(w, req, "/", 302)
		}
		var pages []int
		for i := 0; i <= nPages; i++ {
			pages = append(pages, i)
		}

		// получение списк отмеченных статей
		url = "http://server:12345/account"
		r, err = http.NewRequest("GET", url, nil)
		if err != nil {
			log.Println(err)
			http.Redirect(w, req, "/", 302)
			return
		}
		c = &http.Client{}
		r.Header.Add("auth", token.Value)
		resp, err = c.Do(r)
		if err != nil {
			log.Printf("http.Do() error: %v\n", err)
			http.Redirect(w, req, "/", 302)
			return
		}
		ar, _ = ioutil.ReadAll(resp.Body)
		var user UserPublic
		err = json.Unmarshal(ar, &user)
		if err != nil {
			log.Printf("json unmarshal %v\n", err)
			http.Redirect(w, req, "/", 302)
			return
		}

		var checkAr []bson.ObjectId
		var check []FeedArticle
		for _, i := range user.LikeNews {
			checkAr = append(checkAr, i)
		}
		for _, i := range user.DislikeNews {
			checkAr = append(checkAr, i)
		}
		for _, i := range articles {
			for _, a := range checkAr {
				if i.Id == a {
					check = append(check, FeedArticle{i.Id, true, i.Title, i.Link, i.Source, i.Text, i.Timestamp})
					break
				} else {
					check = append(check, FeedArticle{i.Id, false, i.Title, i.Link, i.Source, i.Text, i.Timestamp})
					break
				}
			}
		}
		t := template.Must(template.ParseFiles(
			"./templates/feed.html",
			"./templates/header.html",
			"./templates/footer.html",
		))

		token, err := req.Cookie("auth")
		var a bool
		if (err != nil) || (token.Value == "") {
			a = false
		} else {
			a = true
		}

		data := struct {
			Art   []FeedArticle
			Pages []int
			Title string
			Auth  bool
		}{
			check,
			pages,
			"Список новостей",
			a,
		}
		err = t.Execute(w, data)
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
	token, err := req.Cookie("auth")
	var a bool
	if (err != nil) || (token.Value == "") {
		a = false
	} else {
		a = true
	}
	data := struct {
		Title string
		Tags  []Tag
		Auth  bool
	}{
		"Авторизация",
		T.Tags,
		a,
	}
	err = t.Execute(w, data)
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
	expiration := time.Now().Add(30 * 24 * time.Hour)
	cookie := http.Cookie{Name: "auth", Value: resp.Header.Get("auth"), Expires: expiration}
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
	expiration := time.Now().Add(30 * 24 * time.Hour)
	cookie := http.Cookie{Name: "auth", Value: resp.Header.Get("auth"), Expires: expiration}
	http.SetCookie(w, &cookie)
	defer resp.Body.Close()
	http.Redirect(w, req, "/feed/0", 302)
}

func logout(w http.ResponseWriter, req *http.Request) {
	cookie := http.Cookie{Name: "auth", Value: "", Expires: time.Now()}
	http.SetCookie(w, &cookie)
	http.Redirect(w, req, "/", 302)
}

package main

import (
	"crypto/rsa"
	"encoding/json"
	"io/ioutil"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/dgrijalva/jwt-go"
	"github.com/gorilla/mux"
	"github.com/rs/cors"
	"gopkg.in/mgo.v2"
	"gopkg.in/mgo.v2/bson"
)

type User struct {
	Id          bson.ObjectId   `bson:"_id,omitempty"`
	Email       string          `bson:"email"`
	Password    string          `bson:"password"`
	Age         string          `bson:"age"`
	Gender      string          `bson:"gender"`
	Tags        []string        `bson:"tags"`
	Feed        []bson.ObjectId `bson:"feed"`
	LikeNews    []bson.ObjectId `bson:"likeNews"`
	DislikeNews []bson.ObjectId `bson:"dislikeNews"`
}

type UserPublic struct {
	Id          bson.ObjectId   `bson:"_id,omitempty"`
	Email       string          `bson:"email"`
	Tags        []string        `bson:"tags"`
	Age         string          `bson:"age"`
	Gender      string          `bson:"gender"`
	Feed        []bson.ObjectId `bson:"feed"`
	LikeNews    []bson.ObjectId `bson:"likeNews"`
	DislikeNews []bson.ObjectId `bson:"dislikeNews"`
}

type Article struct {
	Id        bson.ObjectId `bson:"_id,omitempty"`
	Title     string        `bson:"title"`
	Link      string        `bson:"link"`
	TopImage  string        `bson:"topImage"`
	Source    string        `bson:"source"`
	Tags      []string      `bson:"tags"`
	Text      string        `bson:"text"`
	RawText   string        `bson:"RowText"`
	TextLen   int           `bson:"textLen"`
	NumLinks  int           `bson:"numLinks"`
	NumImg    int           `bson:"numImg"`
	Timestamp time.Time     `bson:"timestamp"`
}

type ArticleFeed struct {
	Id        bson.ObjectId `bson:"_id,omitempty"`
	Title     string        `bson:"title"`
	Checked   bool
	Link      string `bson:"link"`
	TopImage  string
	Source    string    `bson:"source"`
	Text      string    `bson:"text"`
	Timestamp time.Time `bson:"timestamp"`
}

type Token struct {
	Token string `json:"token"`
}

type DataStore struct {
	session *mgo.Session
}

type Claims struct {
	Email string `json:"email"`
	jwt.StandardClaims
}

const (
	privKeyPath = "keys/app.rsa"     // openssl genrsa -out app.rsa keysize
	pubKeyPath  = "keys/app.rsa.pub" // openssl rsa -in app.rsa -pubout > app.rsa.pub
)

var (
	session     *mgo.Session
	verifyKey   *rsa.PublicKey
	signKey     *rsa.PrivateKey
	expireToken = time.Now().Add(time.Hour * 24 * 30).Unix()
)

func init() {
	signBytes, err := ioutil.ReadFile(privKeyPath)
	if err != nil {
		log.Fatal(err)
	}
	signKey, err = jwt.ParseRSAPrivateKeyFromPEM(signBytes)
	if err != nil {
		log.Fatal(err)
	}
	verifyBytes, err := ioutil.ReadFile(pubKeyPath)
	if err != nil {
		log.Fatal(err)
	}
	verifyKey, err = jwt.ParseRSAPublicKeyFromPEM(verifyBytes)
	if err != nil {
		log.Fatal(err)
	}
}

func (ds *DataStore) Close() {
	ds.session.Close()
}

func (ds *DataStore) C(colName string) *mgo.Collection {
	return ds.session.DB("Articles").C(colName)
}

func NewDataStore() *DataStore {
	ds := &DataStore{
		session: session.Copy(),
	}
	return ds
}

func main() {
	log.Println("Server listening")
	var err error
	session, err = mgo.Dial("mongodb://mongo:27017")
	for err != nil {
		session, err = mgo.Dial("mongodb://mongo:27017")
		log.Print(err)
		time.Sleep(time.Second * 5)
	}
	router := mux.NewRouter()
	router.HandleFunc("/login", login).Methods("POST")
	router.HandleFunc("/signup", signup).Methods("POST")
	router.HandleFunc("/ratelike/{id}", restrictedHandler(rateLike)).Methods("POST")
	router.HandleFunc("/ratedislike/{id}", restrictedHandler(rateDislike)).Methods("POST")
	router.HandleFunc("/feed/{page:[0-9]+}", restrictedHandler(feed)).Methods("GET")
	router.HandleFunc("/todayfeed", restrictedHandler(toDayFeed)).Methods("GET")
	router.HandleFunc("/account", restrictedHandler(accountData)).Methods("GET")
	router.HandleFunc("/account/chenge/tags", restrictedHandler(accountTagsChange)).Methods("GET")
	handler := cors.Default().Handler(router)
	log.Fatal(http.ListenAndServe(":12345", handler))
}

// middleware to protect private pages
func restrictedHandler(next http.HandlerFunc) http.HandlerFunc {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		tokenHeader := req.Header.Get("auth")
		if tokenHeader == "" {
			respondWithError(w, http.StatusUnauthorized, "Invalid token")
		}

		token, err := jwt.Parse(tokenHeader, func(token *jwt.Token) (interface{}, error) {
			return verifyKey, nil
		})
		switch err.(type) {
		case nil: // no error
			if !token.Valid { // but may still be invalid
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			// see stdout and watch for the CustomUserInfo, nicely unmarshalled
			next(w, req)

		case *jwt.ValidationError: // something was wrong during the validation
			vErr := err.(*jwt.ValidationError)

			switch vErr.Errors {
			case jwt.ValidationErrorExpired:
				w.WriteHeader(http.StatusUnauthorized)
				return

			default:
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
		default: // something else went wrong
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

	})
}

func signup(w http.ResponseWriter, req *http.Request) {
	// db conection
	ds := NewDataStore()
	defer ds.Close()
	c := ds.C("Users")

	// form parsing
	err := req.ParseForm()
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Can't parse form")
		return
	}
	password := req.FormValue("password")
	email := strings.ToLower(req.FormValue("email"))
	tags := req.Form["tags"]
	age := req.Form["age"]
	gender := req.Form["gender"]

	if (password == "") || (email == "") {
		respondWithError(w, http.StatusBadRequest, "Email or password not specified")
		return
	}
	if (len(tags) == 0) || (age[0] == "") || (gender[0] == "") {
		respondWithError(w, http.StatusBadRequest, "Tags, age or gender not specified")
		return
	}

	// uniqueness check user data
	var user User
	err = c.Find(bson.M{"email": email, "password": password}).One(&user)

	// generate token
	if err == mgo.ErrNotFound {
		err = c.Insert(User{Email: email, Password: password, Tags: tags, Age: age[0], Gender: gender[0]})

		claims := Claims{
			email,
			jwt.StandardClaims{
				ExpiresAt: expireToken,
			},
		}
		token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
		signedToken, err := token.SignedString(signKey)

		if err != nil {
			respondWithError(w, http.StatusBadRequest, "Can't generate token, try again")
			return
		}
		respondWithJSON(w, 200, signedToken)
	} else {
		respondWithError(w, http.StatusBadRequest, "User already registered")
	}
}

func login(w http.ResponseWriter, req *http.Request) {
	// db conection
	ds := NewDataStore()
	defer ds.Close()
	c := ds.C("Users")

	// form parsing
	err := req.ParseForm()
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Can't parse form")
		return
	}
	password := req.FormValue("password")
	email := strings.ToLower(req.FormValue("email"))
	if (password == "") || (email == "") {
		respondWithError(w, http.StatusBadRequest, "Email or password not specified")
		return
	}

	var user User
	err = c.Find(bson.M{"email": email, "password": password}).One(&user)
	if err == mgo.ErrNotFound {
		respondWithError(w, http.StatusBadRequest, "User not found")
		return
	}

	// generate token
	claims := Claims{
		email,
		jwt.StandardClaims{
			ExpiresAt: expireToken,
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	signedToken, err := token.SignedString(signKey)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Can't generate token, try again")
		return
	}
	respondWithJSON(w, 200, signedToken)
}

func rateLike(w http.ResponseWriter, req *http.Request) {
	tokenHeader := req.Header.Get("auth")
	ds := NewDataStore()
	defer ds.Close()
	c := ds.C("Users")

	id := mux.Vars(req)
	token, _ := jwt.ParseWithClaims(tokenHeader, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		return verifyKey, nil
	})
	var user User
	err := c.Find(bson.M{"email": token.Claims.(*Claims).Email}).One(&user)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Can't find user")
		return
	}
	for _, i := range user.DislikeNews {
		if bson.ObjectIdHex(id["id"]) == i {
			return
		}
	}
	err = c.Update(bson.M{"_id": user.Id}, bson.M{"$addToSet": bson.M{"likeNews": bson.ObjectIdHex(id["id"])}})
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Can't add this article")
		return
	}
	respondWithJSON(w, 200, "Successfully added")
}

func rateDislike(w http.ResponseWriter, req *http.Request) {
	tokenHeader := req.Header.Get("auth")
	ds := NewDataStore()
	defer ds.Close()
	c := ds.C("Users")

	id := mux.Vars(req)
	token, _ := jwt.ParseWithClaims(tokenHeader, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		return verifyKey, nil
	})
	var user User
	err := c.Find(bson.M{"email": token.Claims.(*Claims).Email}).One(&user)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Can't find user")
		return
	}
	for _, i := range user.LikeNews {
		if bson.ObjectIdHex(id["id"]) == i {
			return
		}
	}
	err = c.Update(bson.M{"_id": user.Id}, bson.M{"$addToSet": bson.M{"dislikeNews": bson.ObjectIdHex(id["id"])}})
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Can't add this article")
		return
	}
	respondWithJSON(w, 200, "Successfully added")
}

func feed(w http.ResponseWriter, req *http.Request) {
	tokenHeader := req.Header.Get("auth")
	var (
		user  User
		f     []ArticleFeed
		slice [2]int
		// nPage int
	)

	ds := NewDataStore()
	defer ds.Close()
	c := ds.C("Users")
	ca := ds.C("Articles")

	page := mux.Vars(req)
	token, _ := jwt.ParseWithClaims(tokenHeader, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		return verifyKey, nil
	})
	err := c.Find(bson.M{"email": token.Claims.(*Claims).Email}).One(&user)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Can't find user")
		return
	}
	pageInt, err := strconv.Atoi(page["page"])
	if err != nil {
		log.Println(err)
		respondWithError(w, http.StatusBadRequest, "Can't find this page")
	}
	if pageInt == 0 {
		slice[0] = 0
		slice[1] = 10
	} else {
		slice[0] = 0 + 10*(pageInt)
		slice[1] = 10 + 10*(pageInt)
	}

	if len(user.Feed) < slice[1] {
		slice[1] = len(user.Feed)
	}
	// revers feed ids
	var reFeed []bson.ObjectId
	for i := len(user.Feed) - 1; i >= 0; i-- {
		reFeed = append(reFeed, user.Feed[i])
	}
	var checked []bson.ObjectId
	for _, i := range user.LikeNews {
		checked = append(checked, i)
	}
	for _, i := range user.DislikeNews {
		checked = append(checked, i)
	}
	for _, a := range reFeed[slice[0]:slice[1]] {
		var article Article
		err = ca.FindId(a).One(&article)
		if err != nil {
			respondWithError(w, http.StatusBadRequest, "Can't find any of article")
		}
		ch := false
		for _, i := range checked {
			if i == article.Id {
				ch = true
				f = append(f, ArticleFeed{article.Id, article.Title, true, article.Link, article.TopImage, article.Source,
					article.Text, article.Timestamp})
				break
			}
		}
		if ch != true {
			f = append(f, ArticleFeed{article.Id, article.Title, false, article.Link, article.TopImage, article.Source,
				article.Text, article.Timestamp})
		}
	}
	response, err := json.Marshal(f)
	if err != nil {
		log.Println("json marshal: ", err)
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	// nPage = len(user.Feed) / 10
	// w.Header().Set("npage", strconv.Itoa(nPage))
	w.Write(response)
}

func respondWithError(w http.ResponseWriter, code int, message string) {
	respondWithJSON(w, code, map[string]string{"error": message})
}

func respondWithJSON(w http.ResponseWriter, code int, payload interface{}) {
	response, _ := json.Marshal(payload)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	w.Write(response)
}

func accountData(w http.ResponseWriter, req *http.Request) {
	tokenHeader := req.Header.Get("auth")
	ds := NewDataStore()
	defer ds.Close()
	c := ds.C("Users")

	token, _ := jwt.ParseWithClaims(tokenHeader, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		return verifyKey, nil
	})
	var user UserPublic
	err := c.Find(bson.M{"email": token.Claims.(*Claims).Email}).One(&user)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Can't find user")
		return
	}
	respondWithJSON(w, http.StatusOK, user)
}

func toDayFeed(w http.ResponseWriter, req *http.Request) {
	tokenHeader := req.Header.Get("auth")
	var (
		user User
		f    []ArticleFeed
	)

	ds := NewDataStore()
	defer ds.Close()
	c := ds.C("Users")
	ca := ds.C("Articles")

	token, _ := jwt.ParseWithClaims(tokenHeader, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		return verifyKey, nil
	})
	err := c.Find(bson.M{"email": token.Claims.(*Claims).Email}).One(&user)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Can't find user")
		return
	}

	// revers feed ids
	var reFeed []bson.ObjectId
	for i := len(user.Feed) - 1; i >= 0; i-- {
		reFeed = append(reFeed, user.Feed[i])
	}
	var checked []bson.ObjectId
	for _, i := range user.LikeNews {
		checked = append(checked, i)
	}
	for _, i := range user.DislikeNews {
		checked = append(checked, i)
	}
	loc, _ := time.LoadLocation("Europe/Moscow")
	date := time.Now().In(loc).Add(-24 * time.Hour)
	for _, a := range reFeed {
		var article Article
		err = ca.FindId(a).One(&article)
		if err != nil {
			respondWithError(w, http.StatusBadRequest, "Can't find any of article")
		}
		ch := false
		for _, i := range checked {
			if i == article.Id {
				ch = true
				f = append(f, ArticleFeed{article.Id, article.Title, true, article.Link, article.TopImage, article.Source,
					article.Text, article.Timestamp})
				break
			}
		}
		if ch != true {
			f = append(f, ArticleFeed{article.Id, article.Title, false, article.Link, article.TopImage, article.Source,
				article.Text, article.Timestamp})
		}
		if article.Timestamp.Before(date) {
			break
		}
	}
	response, err := json.Marshal(f)
	if err != nil {
		log.Println("json marshal: ", err)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(response)
}

func accountTagsChange(w http.ResponseWriter, req *http.Request) {
	tokenHeader := req.Header.Get("auth")
	ds := NewDataStore()
	defer ds.Close()
	c := ds.C("Users")

	token, _ := jwt.ParseWithClaims(tokenHeader, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		return verifyKey, nil
	})
	var user UserPublic
	err := c.Find(bson.M{"email": token.Claims.(*Claims).Email}).One(&user)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Can't find user")
		return
	}
	err = req.ParseForm()
	if err != nil {
		log.Println(err)
	}
	tags := req.Form["tags"]
	c.Update(bson.M{"_id": user.Id}, bson.M{"$set": bson.M{"tags": tags}})
}

// tokenHeader, err := req.Cookie("Auth")
// switch {
// case err == http.ErrNoCookie:
// 	w.WriteHeader(http.StatusUnauthorized)
// 	return
// case err != nil:
// 	w.WriteHeader(http.StatusInternalServerError)
// 	return
// }
// // just for the lulz, check if it is empty.. should fail on Parse anyway..
// if tokenHeader.Value == "" {
// 	w.WriteHeader(http.StatusUnauthorized)
// 	return
// }
// // validate the token
// token, err := jwt.Parse(tokenHeader.Value, func(token *jwt.Token) (interface{}, error) {
// 	return verifyKey, nil
// })
// // branch out into the possible error from signing
// switch err.(type) {
// case nil: // no error
// 	if !token.Valid { // but may still be invalid
// 		w.WriteHeader(http.StatusUnauthorized)
// 		return
// 	}
// 	// see stdout and watch for the CustomUserInfo, nicely unmarshalled
// 	next(w, req)

// case *jwt.ValidationError: // something was wrong during the validation
// 	vErr := err.(*jwt.ValidationError)

// 	switch vErr.Errors {
// 	case jwt.ValidationErrorExpired:
// 		w.WriteHeader(http.StatusUnauthorized)
// 		return

// 	default:
// 		w.WriteHeader(http.StatusInternalServerError)
// 		return
// 	}

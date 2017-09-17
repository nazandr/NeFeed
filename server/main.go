package main

import (
	"crypto/rsa"
	"encoding/json"
	"io/ioutil"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/dgrijalva/jwt-go"
	"github.com/gorilla/mux"
	"gopkg.in/mgo.v2"
	"gopkg.in/mgo.v2/bson"
)

type User struct {
	Id          bson.ObjectId   `bson:"_id,omitempty"`
	Email       string          `bson:"email"`
	Password    string          `bson:"password"`
	Tags        []string        `bson:"tags"`
	Feed        []bson.ObjectId `bson:"feed"`
	LikeNews    []bson.ObjectId `bson:"likeNews"`
	DislikeNews []bson.ObjectId `bson:"dislikeNews"`
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
	router.HandleFunc("/auth", auth).Methods("POST")                  //login
	router.Handle("/auth", auth).Methods("PUT")                       //singup
	router.Handle("/auth", restrictedHandler(auth)).Methods("DELETE") //logout
	router.Handle("/ratelike/{id}", restrictedHandler(rateLike)).Methods("POST")
	router.Handle("ratedislike/{id}", restrictedHandler(rateDislike)).Methods("POST")
	router.HandleFunc("/feed/{page:[0-9]+}", restrictedHandler(feed)).Methods("GET")
	log.Fatal(http.ListenAndServe(":12345", router))
	// err = http.ListenAndServeTLS(":12345", "./keys/server.crt", "./keys/server.key", http.Handler(router))

	// if err != nil {
	// 	log.Fatal("ListenAndServe: ", err)
	// }
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

var auth = http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
	ds := NewDataStore()
	defer ds.Close()
	c := ds.C("Users")

	switch req.Method {
	default:
		http.Error(w, "Method Not Allowed", 405)
	case "POST": //LogIn
		err := req.ParseForm()
		if err != nil {
			log.Println(err)
		}
		password := req.FormValue("password")
		email := req.FormValue("email")
		log.Println(email, password)
		var user User
		err = c.Find(bson.M{"email": email, "password": password}).One(&user)
		if err == mgo.ErrNotFound {
			respondWithError(w, http.StatusBadRequest, "User not found")
			return
		}
		if err != nil {
			log.Fatal(err)
		}

		claims := Claims{
			email,
			jwt.StandardClaims{
				ExpiresAt: expireToken,
			},
		}

		token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)

		signedToken, err := token.SignedString(signKey)

		if err != nil {
			log.Fatal(err)
		}
		w.Header().Set("auth", signedToken)
		w.WriteHeader(200)
		log.Println(signedToken)

	case "DELETE":
		w.Header().Add("auth", "")
		w.WriteHeader(200)
		respondWithJSON(w, 200, "log out")
		return
	case "PUT":
		err := req.ParseForm()
		if err != nil {
			log.Println(err)
		}
		password := req.FormValue("password")
		email := req.FormValue("email")
		log.Println(email, password)
		tags := req.Form["tags"]
		var user User
		err = c.Find(bson.M{"email": email, "password": password}).One(&user)
		log.Println(user)
		log.Println(err)
		if err == mgo.ErrNotFound {
			err = c.Insert(User{Email: email, Password: password, Tags: tags})

			claims := Claims{
				email,
				jwt.StandardClaims{
					ExpiresAt: expireToken,
				},
			}

			token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)

			signedToken, err := token.SignedString(signKey)

			if err != nil {
				log.Fatal(err)
			}
			w.Header().Set("auth", signedToken)
			w.WriteHeader(200)
		} else {
			respondWithError(w, http.StatusBadRequest, "User already iu use")
			return
		}
	}
})

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
		f     []Article
		slice [2]int
		nPage int
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
		slice[1] = 20
	} else {
		slice[0] = 0 + 20*(pageInt)
		slice[1] = 20 + 20*(pageInt)
	}

	if len(user.Feed) < slice[1] {
		slice[1] = len(user.Feed)
	}
	// revers feed ids
	var reFeed []bson.ObjectId
	for i := len(user.Feed) - 1; i >= 0; i-- {
		reFeed = append(reFeed, user.Feed[i])
	}

	for _, a := range reFeed[slice[0]:slice[1]] {
		var article Article
		err = ca.FindId(a).One(&article)
		if err != nil {
			respondWithError(w, http.StatusBadRequest, "Can't find any of article")
		}
		f = append(f, article)
	}
	response, _ := json.Marshal(f)
	w.Header().Set("Content-Type", "application/json")
	nPage = len(user.Feed) / 20
	w.Header().Set("npage", strconv.Itoa(nPage))
	w.WriteHeader(http.StatusOK)
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

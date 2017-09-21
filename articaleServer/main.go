package main

import (
	"encoding/json"
	"hash/crc32"
	"io/ioutil"
	"log"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/mauidude/go-readability"
	"github.com/mmcdole/gofeed"
	"github.com/streadway/amqp"
	"gopkg.in/mgo.v2"
	"gopkg.in/mgo.v2/bson"
)

type Article struct {
	Id              bson.ObjectId   `bson:"_id,omitempty"`
	Title           string          `bson:"title"`
	Link            string          `bson:"link"`
	Source          string          `bson:"source"`
	Tags            []string        `bson:"tags"`
	Text            string          `bson:"text"`
	TextLen         int             `bson:"textLen"`
	NumLinks        int             `bson:"numLinks"`
	NumImg          int             `bson:"numImg"`
	AverCommentSent int             `bson:"averCommentSent"`
	Duplicates      []bson.ObjectId `bson:"duplicates"`
	Shingle         []uint32        `bson:"shingle"`
	Timestamp       time.Time       `bson:"timestamp"`
}

type Source struct {
	Name string   `json:"Name"`
	Tags []string `json:"Tags"`
	RSS  string   `json:"RSS"`
}
type Item struct {
	Url    string
	Source Source
}
type DataStore struct {
	session *mgo.Session
}

var (
	session  *mgo.Session
	rabbitCh *amqp.Channel
	sources  []Source
	lastPost = make(map[string]string)
	links    []string
)

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

func init() {
	file, err := ioutil.ReadFile("./sources.json")
	if err != nil {
		log.Fatal(err)
	}

	json.Unmarshal(file, &sources)
	for _, i := range sources {
		links = append(links, i.Name)
		lastPost[i.Name] = ""
	}
}

func main() {
	newItem := make(chan Item, 3)
	var err error

	rabbitConn, err := amqp.Dial("amqp://rabbitmq:5672")
	for err != nil {
		rabbitConn, err = amqp.Dial("amqp://rabbitmq:5672")
		log.Print(err)
		time.Sleep(time.Second * 5)
	}
	if err != nil {
		log.Fatal("rabbitmq conn err: ", err)
	}
	defer rabbitConn.Close()
	rabbitCh, err = rabbitConn.Channel()
	if err != nil {
		log.Fatal("rabbitmq ch err: ", err)
	}
	defer rabbitCh.Close()
	_, err = rabbitCh.QueueDeclare(
		"butler", // name
		true,     // durable
		false,    // delete when unused
		false,    // exclusive
		false,    // no-wait
		nil,      // arguments
	)
	if err != nil {
		log.Fatal("rabbitmq q err: ", err)
	}

	session, err = mgo.Dial("mongodb://mongo:27017")
	for err != nil {
		session, err = mgo.Dial("mongodb://mongo:27017")
		log.Print(err)
		time.Sleep(time.Second * 5)
	}

	for _, i := range sources {
		go Handler(i, newItem)
	}

	go Manager(newItem)

	timer := time.NewTicker(time.Minute * 5)
	for range timer.C {
		for _, i := range sources {
			go Handler(i, newItem)
		}
	}

}

func Manager(newItem chan Item) {
	for {
		select {
		case res := <-newItem:
			log.Print(res.Url)
			go Parser(res)
		}
	}
}

func forBatler(id bson.ObjectId) {
	err := rabbitCh.Publish(
		"",
		"butler",
		false,
		false,
		amqp.Publishing{
			DeliveryMode: amqp.Persistent,
			ContentType:  "text/plain",
			Body:         []byte(id),
		},
	)
	if err != nil {
		log.Println("forBatler err: ", err)
	}
}

func Handler(item Source, newItem chan<- Item) {
	ds := NewDataStore()
	defer ds.Close()
	c := ds.C("Articles")

	parser := gofeed.NewParser()
	val, err := parser.ParseURL(item.RSS)
	for err != nil {
		log.Println("handler parser err: ", err)
		return
	}

	var art Article
	link := val.Items[0].Link
	err = c.Find(bson.M{"link": link}).One(&art)
	if err == mgo.ErrNotFound {
		newItem <- Item{link, item}
	}
}

// Parser parse new news from Handler
func Parser(item Item) {
	ds := NewDataStore()
	defer ds.Close()
	c := ds.C("Articles")
	v, err := goquery.NewDocument(item.Url)
	for err != nil {
		v, err = goquery.NewDocument(item.Url)
		log.Println("parser goquery err: ", err)
		time.Sleep(time.Second * 5)
	}
	html, _ := v.Html()
	d, _ := readability.NewDocument(html)
	title := v.Find("title").Text()
	text := d.Content()
	text = strings.TrimPrefix(text, "<html><head></head><body><div>")
	text = strings.TrimSuffix(text, "</div></div></body></html>")
	textLen := len(text)
	numLinks := strings.Count(text, "<a")
	numImg := strings.Count(text, "<img")
	shingle, duplicates := searchDuplicates(text, c)
	loc, _ := time.LoadLocation("Europe/Moscow")
	err = c.Insert(Article{Title: title, Link: item.Url, Source: item.Source.Name, Tags: item.Source.Tags, Text: text, TextLen: textLen,
		NumLinks: numLinks, NumImg: numImg, Timestamp: time.Now().In(loc), Shingle: shingle, Duplicates: duplicates})
	if err != nil {
		log.Println("Insert err: ", err)
	}
	var a Article
	err = c.Find(bson.M{"link": item.Url}).One(&a)
	if err != nil {
		log.Println("parser find err: ", err)
	}
	go forBatler(a.Id)
}

func searchDuplicates(text string, col *mgo.Collection) (shingle []uint32, duplicates []bson.ObjectId) {
	stopWords := [559]string{"c", "а", "алло", "без", "белый", "близко", "более", "больше", "большой", "будем", "будет", "будете", "будешь", "будто", "буду", "будут", "будь", "бы", "бывает", "бывь", "был", "была", "были", "было", "быть", "в", "важная", "важное", "важные", "важный", "вам", "вами", "вас", "ваш", "ваша", "ваше", "ваши", "вверх", "вдали", "вдруг", "ведь", "везде", "вернуться", "весь", "вечер", "взгляд", "взять", "вид", "видел", "видеть", "вместе", "вне", "вниз", "внизу", "во", "вода", "война", "вокруг", "вон", "вообще", "вопрос", "восемнадцатый", "восемнадцать", "восемь", "восьмой", "вот", "впрочем", "времени", "время", "все", "все еще", "всегда", "всего", "всем", "всеми", "всему", "всех", "всею", "всю", "всюду", "вся", "всё", "второй", "вы", "выйти", "г", "где", "главный", "глаз", "говорил", "говорит", "говорить", "год", "года", "году", "голова", "голос", "город", "да", "давать", "давно", "даже", "далекий", "далеко", "дальше", "даром", "дать", "два", "двадцатый", "двадцать", "две", "двенадцатый", "двенадцать", "дверь", "двух", "девятнадцатый", "девятнадцать", "девятый", "девять", "действительно", "дел", "делал", "делать", "делаю", "дело", "день", "деньги", "десятый", "десять", "для", "до", "довольно", "долго", "должен", "должно", "должный", "дом", "дорога", "друг", "другая", "другие", "других", "друго", "другое", "другой", "думать", "душа", "е", "его", "ее", "ей", "ему", "если", "есть", "еще", "ещё", "ею", "её", "ж", "ждать", "же", "жена", "женщина", "жизнь", "жить", "за", "занят", "занята", "занято", "заняты", "затем", "зато", "зачем", "здесь", "земля", "знать", "значит", "значить", "и", "иди", "идти", "из", "или", "им", "имеет", "имел", "именно", "иметь", "ими", "имя", "иногда", "их", "к", "каждая", "каждое", "каждые", "каждый", "кажется", "казаться", "как", "какая", "какой", "кем", "книга", "когда", "кого", "ком", "комната", "кому", "конец", "конечно", "которая", "которого", "которой", "которые", "который", "которых", "кроме", "кругом", "кто", "куда", "лежать", "лет", "ли", "лицо", "лишь", "лучше", "любить", "люди", "м", "маленький", "мало", "мать", "машина", "между", "меля", "менее", "меньше", "меня", "место", "миллионов", "мимо", "минута", "мир", "мира", "мне", "много", "многочисленная", "многочисленное", "многочисленные", "многочисленный", "мной", "мною", "мог", "могу", "могут", "мож", "может", "может быть", "можно", "можхо", "мои", "мой", "мор", "москва", "мочь", "моя", "моё", "мы", "на", "наверху", "над", "надо", "назад", "наиболее", "найти", "наконец", "нам", "нами", "народ", "нас", "начала", "начать", "наш", "наша", "наше", "наши", "не", "него", "недавно", "недалеко", "нее", "ней", "некоторый", "нельзя", "нем", "немного", "нему", "непрерывно", "нередко", "несколько", "нет", "нею", "неё", "ни", "нибудь", "ниже", "низко", "никакой", "никогда", "никто", "никуда", "ним", "ними", "них", "ничего", "ничто", "но", "новый", "нога", "ночь", "ну", "нужно", "нужный", "нх", "о", "об", "оба", "обычно", "один", "одиннадцатый", "одиннадцать", "однажды", "однако", "одного", "одной", "оказаться", "окно", "около", "он", "она", "они", "оно", "опять", "особенно", "остаться", "от", "ответить", "отец", "откуда", "отовсюду", "отсюда", "очень", "первый", "перед", "писать", "плечо", "по", "под", "подойди", "подумать", "пожалуйста", "позже", "пойти", "пока", "пол", "получить", "помнить", "понимать", "понять", "пор", "пора", "после", "последний", "посмотреть", "посреди", "потом", "потому", "почему", "почти", "правда", "прекрасно", "при", "про", "просто", "против", "процентов", "путь", "пятнадцатый", "пятнадцать", "пятый", "пять", "работа", "работать", "раз", "разве", "рано", "раньше", "ребенок", "решить", "россия", "рука", "русский", "ряд", "рядом", "с", "с кем", "сам", "сама", "сами", "самим", "самими", "самих", "само", "самого", "самой", "самом", "самому", "саму", "самый", "свет", "свое", "своего", "своей", "свои", "своих", "свой", "свою", "сделать", "сеаой", "себе", "себя", "сегодня", "седьмой", "сейчас", "семнадцатый", "семнадцать", "семь", "сидеть", "сила", "сих", "сказал", "сказала", "сказать", "сколько", "слишком", "слово", "случай", "смотреть", "сначала", "снова", "со", "собой", "собою", "советский", "совсем", "спасибо", "спросить", "сразу", "стал", "старый", "стать", "стол", "сторона", "стоять", "страна", "суть", "считать", "т", "та", "так", "такая", "также", "таки", "такие", "такое", "такой", "там", "твои", "твой", "твоя", "твоё", "те", "тебе", "тебя", "тем", "теми", "теперь", "тех", "то", "тобой", "тобою", "товарищ", "тогда", "того", "тоже", "только", "том", "тому", "тот", "тою", "третий", "три", "тринадцатый", "тринадцать", "ту", "туда", "тут", "ты", "тысяч", "у", "увидеть", "уж", "уже", "улица", "уметь", "утро", "хороший", "хорошо", "хотел бы", "хотеть", "хоть", "хотя", "хочешь", "час", "часто", "часть", "чаще", "чего", "человек", "чем", "чему", "через", "четвертый", "четыре", "четырнадцатый", "четырнадцать", "что", "чтоб", "чтобы", "чуть", "шестнадцатый", "шестнадцать", "шестой", "шесть", "эта", "эти", "этим", "этими", "этих", "это", "этого", "этой", "этом", "этому", "этот", "эту", "я", "являюсь"}
	for _, i := range stopWords {
		text = strings.Trim(text, i)
	}
	shingleLen := 10
	seq := strings.Split(text, " ")
	for i := 0; i < (len(seq) - (shingleLen - 1)); i++ {
		h := crc32.NewIEEE()
		h.Write([]byte(strings.Join(seq[i:i+shingleLen], " ")))
		shingle = append(shingle, h.Sum32())
	}
	t := time.Now().Add(-48 * time.Hour).String()
	var date []Article
	err := col.Find(bson.M{"Timestamp": bson.M{"$lt": t}}).All(&date)
	if err != nil {
		log.Print(err)
	}

	for _, article := range date {
		rate := 0.0
		for i := 0; i < len(shingle); i++ {
			if in(shingle[i], article.Shingle) {
				rate++
			}
		}
		rate = rate * 2 / float64(len(shingle)+len(article.Shingle)) * 100
		if rate > 70.0 {
			duplicates = append(duplicates, article.Id)
		}
	}

	return
}

func in(a uint32, list []uint32) bool {
	for _, b := range list {
		if b == a {
			return true
		}
	}
	return false
}

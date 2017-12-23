from time import sleep
import re
import logging
from html import unescape

from newspaper import Article
from sanic import Sanic
from sanic.response import json

app = Sanic()


@app.route("/link/<link>")
async def main(request, link):
    link = re.sub(r"&&&", "/", link)
    print(link)
    a = Article(link, keep_article_html=True)
    a.download()
    a.parse()

    return json({"title": a.title, "text": a.text, "rawText": unescape(a.article_html), "topImage": a.top_image, "numLinks": len(re.findall("<a", a.article_html)),
                 "numImage": len(re.findall("<img", a.article_html))})

from sanic.response import text

if __name__ == "__main__":
    app.run(host='0.0.0.0', port=8000, debug=True)


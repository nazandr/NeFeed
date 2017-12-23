import pika
from pymongo import MongoClient
from bson.objectid import ObjectId
from time import sleep

# Mongodb connection
client = MongoClient("mongodb://mongo:27017")
db = client.Articles
userColl = db.Users
articlesColl = db.Articles

# RabbitMq connection
try:
    connection = pika.BlockingConnection(pika.URLParameters("amqp://rabbitmq:5672"))
except pika.exceptions.ConnectionClosed:
    sleep(5)
    try:
        connection = pika.BlockingConnection(pika.URLParameters("amqp://rabbitmq:5672"))
    except pika.exceptions.ConnectionClosed:
        sleep(5)
        connection = pika.BlockingConnection(pika.URLParameters("amqp://rabbitmq:5672"))

channel = connection.channel()

channel.queue_declare(queue='butler', durable=True)
def callback(ch, method, properties, body):
    _id = body
    print(_id)
    articel = articlesColl.find_one({'_id': ObjectId(_id)})
    print(articel["tags"])
    for tag in articel["tags"]:
        userColl.update_many({"tags": tag}, {"$addToSet": {"feed": ObjectId(_id)}})
        
    ch.basic_ack(delivery_tag = method.delivery_tag)

channel.basic_qos(prefetch_count=1)
channel.basic_consume(callback, queue='butler')

channel.start_consuming()

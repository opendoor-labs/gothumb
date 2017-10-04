import hmac
import base64
import hashlib

def generate_signature(message, key):
    key = bytes(key, 'UTF-8')
    message = bytes(message, 'UTF-8')
    digester = hmac.new(key, message, hashlib.sha1)
    signature1 = digester.digest()
    signature2 = base64.urlsafe_b64encode(signature1)
    return str(signature2, 'UTF-8')

generate_signature('300x0/https://listing-photos-production.s3.amazonaws.com/uploads/listing-4772/1973884-RCle1Lr6Q2w.jpg', SECURITY_KEY)

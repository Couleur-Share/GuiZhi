"""对独立测试容器执行重启持久化验证；所有内容和凭证均为合成数据。"""
import json, secrets, subprocess, time, urllib.request, uuid, sys
container = sys.argv[1]
origin = 'https://capture.test'
base = 'http://127.0.0.1:48788'
def request(path, body=None, credential=None, method=None):
    headers={'Content-Type':'application/json','X-Guizhi-Protocol':'1','Origin':origin,'X-Guizhi-Csrf':'1'}
    if credential: headers['Authorization']='Bearer '+credential
    req=urllib.request.Request(base+path,data=json.dumps(body).encode() if body is not None else None,headers=headers,method=method)
    with urllib.request.urlopen(req,timeout=20) as response: return json.load(response)
for attempt in range(30):
    try: request('/healthz'); break
    except Exception: time.sleep(1)
else: raise RuntimeError('测试容器未就绪')
invite=subprocess.check_output(['docker','exec',container,'node','dist/admin.js','invite'],text=True).strip()
desktop=secrets.token_urlsafe(32)
request('/v1/mailboxes',{'invite':invite,'credential':desktop,'requestId':str(uuid.uuid4())})
nonce=secrets.token_urlsafe(32); pairing=request('/v1/pairings',{'nonce':nonce},desktop)
phone=secrets.token_urlsafe(32)
claim=request('/v1/pairings/claim',{'pairingId':pairing['id'],'nonce':nonce,'credential':phone,'name':'合成 Docker 手机'})
request('/v1/pairings/'+pairing['id']+'/confirm',{'deviceId':claim['id']},desktop)
body={'requestId':str(uuid.uuid4()),'input':'Docker 重启合成文字','mode':'text'}
receipt=request('/v1/captures',body,phone)
subprocess.run(['docker','restart',container],check=True,stdout=subprocess.DEVNULL)
for attempt in range(30):
    try: request('/healthz'); break
    except Exception: time.sleep(1)
assert request('/v1/captures',body,phone)['id']==receipt['id']
assert len(request('/v1/deliveries',credential=desktop))==1
request('/v1/deliveries/'+receipt['id']+'/ack',{},desktop)
assert len(request('/v1/deliveries',credential=desktop))==0
request('/v1/deliveries/'+receipt['id']+'/ack',{},desktop)
print('Docker 重启、响应重放和 ACK 重放验证通过')

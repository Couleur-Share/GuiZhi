"""生成不含服务地址和用户凭证的快捷指令源文件，签名必须在 macOS 完成。"""
import plistlib
from pathlib import Path
from uuid import uuid5, NAMESPACE_URL
def uid(name): return str(uuid5(NAMESPACE_URL, 'guizhi-capture/'+name)).upper()
def text(value, attachments=None):
    result = {'string': value}
    if attachments: result['attachmentsByRange'] = attachments
    return {'Value': result, 'WFSerializationType': 'WFTextTokenString'}
def output(name): return {'Type': 'ActionOutput', 'OutputUUID': uid(name), 'OutputName': name}
def ref(name): return {'Value': output(name), 'WFSerializationType': 'WFTextTokenAttachment'}
def token(name, prefix='', suffix=''): return text(prefix+'\ufffc'+suffix, {'{%d, 1}'%len(prefix): output(name)})
def dictionary(values):
    return {'Value': {'WFDictionaryFieldValueItems': [{'WFKey':text(k),'WFItemType':0,'WFValue':text(v) if isinstance(v,str) else v} for k,v in values.items()]}, 'WFSerializationType':'WFDictionaryFieldValue'}
def action(name, identifier, **params): return {'WFWorkflowActionIdentifier':'is.workflow.actions.'+identifier,'WFWorkflowActionParameters':{'UUID':uid(name),**params}}
source = {
 'WFWorkflowName':'发送到归知', 'WFWorkflowClientRelease':'18.0', 'WFWorkflowMinimumClientVersion':900,
 'WFWorkflowIcon':{'WFWorkflowIconStartColor': 4282601983, 'WFWorkflowIconGlyphNumber':61440},
 'WFWorkflowTypes':['ActionExtension'], 'WFWorkflowInputContentItemClasses':['WFURLContentItem','WFTextContentItem'],
 'WFWorkflowHasShortcutInputVariables':True,
 'WFWorkflowImportQuestions':[
   {'ActionIndex':0,'Category':'Parameter','ParameterKey':'WFTextActionText','Text':'填写归知收集服务根地址（HTTPS，末尾不要斜杠）','DefaultValue':'https://capture.example.com'},
   {'ActionIndex':1,'Category':'Parameter','ParameterKey':'WFTextActionText','Text':'粘贴手机收集网页生成的专用快捷指令凭证','DefaultValue':'REPLACE_WITH_SHORTCUT_CREDENTIAL'}],
 'WFWorkflowActions':[
   action('服务地址','gettext',WFTextActionText='https://capture.example.com'),
   action('投递凭证','gettext',WFTextActionText='REPLACE_WITH_SHORTCUT_CREDENTIAL'),
   action('分享内容','gettext',WFTextActionText=text('\ufffc',{'{0, 1}':{'Type':'ExtensionInput'}})),
   action('请求编号','generateuuid'),
   action('投递结果','downloadurl',WFURL=token('服务地址',suffix='/v1/captures'),WFHTTPMethod='POST',WFHTTPBodyType='JSON',ShowHeaders=True,
     WFHTTPHeaders=dictionary({'Authorization':token('投递凭证',prefix='Bearer '),'X-Guizhi-Protocol':'1','Content-Type':'application/json'}),
     WFJSONValues=dictionary({'requestId':token('请求编号'),'input':token('分享内容'),'mode':'auto'})),
   action('显示回执','showresult',Text=token('投递结果',prefix='归知接收回执（accepted / received 表示已接收；error 表示失败）：\n')),
 ]}
directory = Path(__file__).resolve().parent
(directory/'Send-to-GuiZhi.plist').write_bytes(plistlib.dumps(source, fmt=plistlib.FMT_XML, sort_keys=False))
(directory/'Send-to-GuiZhi.unsigned.shortcut').write_bytes(plistlib.dumps(source, fmt=plistlib.FMT_BINARY, sort_keys=False))

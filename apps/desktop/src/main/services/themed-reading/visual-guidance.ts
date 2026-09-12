export const READING_GRAPHICS_GUIDANCE = `可选视觉定义：返回JSON可增加visuals:[]和animations:[]。按内容选择，不强制配图；每页最多8个图形、3个自动动画。
所有图形使用唯一容器<figure data-reading-visual="flow"><figcaption>图形完整文字说明</figcaption></figure>，figcaption不能省略；编译失败仍显示该说明。标题说明必须与正文一致。容器内不能再放其他视觉容器。
visuals项目：{id:"flow",kind:"mermaid",title:"过程",description:"解释",source:"flowchart TD\\nA[开始] --> B[结束]"}。支持flowchart、sequenceDiagram、stateDiagram-v2、mindmap；优先纵向。不能提供init、frontmatter、click、classDef、style、HTML、URL或外部资源。
有真实数据时使用{ id:"chart1",kind:"chart",title,description,chart:{type:"bar"|"line"|"area"|"pie"|"donut",categories:["甲","乙"],series:[{name:"数量",values:[10,20]}],unit:"个",evidence:{section:0,quote:"编辑稿中包含这些数字的原句"}}}。section为manuscript的零基序号，quote必须逐字存在。最多6系列、合计200个值；饼图只接受一个非负系列。无依据不得画定量图。
自定义SVG：{id:"custom1",kind:"svg",title,description}，对应figure内放完整安全SVG与figcaption，静态就能理解全部内容。动画只对这种SVG定义，不能依赖Mermaid内部自动ID。
animations项目：{visualId:"custom1",preset:"draw"|"reveal"|"motion"|"morph",targetId:"SVG元素ID",pathId?:"同一SVG内路径ID",duration?:1000,order?:0}。draw目标必须path；motion需pathId轨道；morph目标和pathId均为path。reveal只动画图形，不能隐藏文字标签。每元素只定义一次。单步400-1600毫秒，单图总时长不超过8000毫秒。不能输出脚本、事件、CSS keyframes或无限循环。`;

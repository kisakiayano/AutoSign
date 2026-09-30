# Auto Sign
还在为包干区不想去，pc端签到怕忘记签退而烦恼?

使用Auto Sign吧,一次签到，永久轮椅

妈妈再也不用担心我因为没有签到签退而被扣分了(

## 功能
本项目基于Go写了一个简单的发包脚本+模拟了金数据的点击地图获取经纬度数据

同时为了防止不同需求下签到签退的时间间隔可能不相同

把sleep time换成了可以自己指定的数值



## 依赖

用编译好的版本的话无需依赖，开盖即食用

需要对源码自己做调整的需要下一下gjson的依赖

## 使用方法

```
AutoSign.exe [sleep time] [签到URL] [签退URL]  [班级] [姓名] [学号] [周几]
```

例如:

```
AutoSign.exe 1 https://oekbteva.jsjform.com/f/qyV2W8  https://oekbteva.jsjform.com/f/qyV2W8 软工114 AyaN0 1919810 周日
```

`sleep time` 的基本单位是分钟

后面四个参数可以按表单的填写顺序自己调整

如需通过源码重新自己编译的话

在项目目录下运行以下命令即可

```
go build -trimpath -o AutoSign.exe
```



在成功提交过一次后会把个人的基本信息存储为json到本地

这样下次运行就只要前三个参数就可以啦

```
AutoSign.exe 1 https://oekbteva.jsjform.com/f/qyV2W8  https://oekbteva.jsjform.com/f/qyV2W8
```


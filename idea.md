import "/sss/sss/a"
import b "/sss/sss/b" //也可以是一个各种资源文件（js，shell，markdown，json，yaml等）， 编译的时候自动吧资源编译对象， 不用 采用类似 Self: 方式。 这样更加智能自动。 支持合并成一个*.lm统一文件，页可以吧各种资源分离分离出去

fn setOverview(a,b){
    base.Overview.after(self.job,self.ddd,self.markdown.job)     引用我们的一个节 自动推断类型
    base.Overview.after(self.markdown.job,self.json.job)         引用我们的一个节
    base.Overview.after(self.n1.job)          引用我们的一个节
    base.Overview.after(self.n2.json)         引用我们的一个节
    base.Overview.after(a.json)         引用import我们的一个节
    base.Overview.after(b.json)         引用import我们的一个节

    return true
}

fn settitle(a,b){
    a=base.title.after("Where this fits") // 没有self，直接插入字符串
    if !a {   
       return err.format("ddd: %s",a)
    }
    //  多行 没有self，直接插入字符串
    base.title.after{
        ```
            Where this fits
            sdfsfs sfsdf

        ```
    } 
}

fn main(a,b){
   setOverview(a,b)
   settitle(a,b)
}

main("a","b")

---
Self:
    ```markdown
     # job
        222sfsfsdf
    ```

    ```json
     {
        "ddd":11
     }
    ```
---

Self as n1:
    ```markdown
     # job
        222sfsfsdf
    ```
---

Self as n2:
    ```json
     {
        "ddd":11
     }
    ```  


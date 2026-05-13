config.yaml中关于filters的配置注意事项：

namespace: 为空或.*
exclude_namespace: 非空
扫描排除后的所有命名空间

namespace: 为空
exclude_namespace: 为空
扫描所有命名空间

namespace: 为空
exclude_namespace: .*
排除所有命名空间

namespace: 非空
exclude_namespace: .*
排除所有命名空间

namespace: 非空
exclude_namespace: 为空
扫描指定命名空间

namespace: 与exclude相同
exclude_namespace: 与exclude相同
排除命名空间，没有数据

####################################
deployment：为空或namespace/.*
eclude_deployment：为空
扫描指定命名空间里的所有deploy

deployment：为空
eclude_deployment：namespace/.*
排除指定命名空间里的所有deploy

deployment：（同一命名空间）namespace/.*
eclude_deployment：（同一命名空间）namespace/.*
排除指定命名空间里的所有deploy

deployment：（同一命名空间）namespace/.*
eclude_deployment：（同一命名空间）非空
扫描排除后的匹配的deploy

deployment：非空（同一命名空间）
eclude_deployment：非空（同一命名空间）
扫描排除后的匹配的deploy


deployment：空（同一命名空间）
eclude_deployment：非空（同一命名空间）
扫描排除后的匹配的deploy

######################################
同时存在多条不同命名空间的包含和过滤规则时：

情况1：只需更新指定的deploy规则，在exclude中可以不配置
情况2：只需排除指定的deploy规则，在include中需要配置：namepace/.*或者namespace/xxxx，否则会导致都不匹配
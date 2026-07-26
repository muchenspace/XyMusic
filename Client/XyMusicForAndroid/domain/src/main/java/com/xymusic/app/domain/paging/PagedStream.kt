package com.xymusic.app.domain.paging

/**
 * 领域层分页数据流句柄。
 *
 * 领域端口只承诺"可分页地提供 T"，不暴露任何具体分页运行时类型；
 * 具体实现由基础设施层提供，并在 data/presentation 边界转换为平台分页类型。
 */
interface PagedStream<T>

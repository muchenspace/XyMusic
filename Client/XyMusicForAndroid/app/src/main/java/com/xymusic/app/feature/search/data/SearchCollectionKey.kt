package com.xymusic.app.feature.search.data

import com.xymusic.app.feature.search.domain.model.SearchQuery
import java.security.MessageDigest

internal fun SearchQuery.searchCollectionKey(): String = "search:v1:${queryDigest()}"

private fun SearchQuery.queryDigest(): String = MessageDigest
    .getInstance("SHA-256")
    .digest(normalizedValue.encodeToByteArray())
    .joinToString(separator = "") { byte -> "%02x".format(byte.toInt() and 0xff) }

package com.xymusic.app.testing

import android.app.Application
import android.content.ComponentName
import androidx.activity.ComponentActivity
import org.robolectric.Shadows

class ComposeTestApplication : Application() {
    override fun onCreate() {
        super.onCreate()
        Shadows
            .shadowOf(packageManager)
            .addActivityIfNotPresent(ComponentName(this, ComponentActivity::class.java))
            .apply {
                exported = true
                theme = android.R.style.Theme_Material_Light_NoActionBar
            }
    }
}

package com.sahandfarshad.minimonitor

import android.annotation.SuppressLint
import android.app.Activity
import android.graphics.Color
import android.os.Bundle
import android.webkit.JavascriptInterface
import android.webkit.WebView
import org.json.JSONObject
import java.util.concurrent.Executors

// Shell around the shared Go core (mobile.aar) and the shared HTML frontend.
class MainActivity : Activity() {
    private lateinit var web: WebView
    private val pool = Executors.newCachedThreadPool()
    private var paused = false

    @SuppressLint("SetJavaScriptEnabled")
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        val bg = Color.parseColor("#11151c")
        window.statusBarColor = bg
        window.navigationBarColor = bg
        web = WebView(this)
        web.setBackgroundColor(bg)
        web.settings.javaScriptEnabled = true
        web.settings.domStorageEnabled = true
        web.settings.allowFileAccess = true
        web.addJavascriptInterface(Bridge(), "AndroidBridge")
        setContentView(web)

        mobile.Mobile.start(filesDir.absolutePath, BuildConfig.VERSION_NAME, object : mobile.Listener {
            override fun onEvent(name: String, json: String) {
                js("window.__mmEmit && window.__mmEmit(${JSONObject.quote(name)}, ${JSONObject.quote(json)})")
            }
        })
        web.loadUrl("file:///android_asset/www/index.html")
    }

    private fun js(code: String) {
        web.post { web.evaluateJavascript(code, null) }
    }

    inner class Bridge {
        @JavascriptInterface
        fun call(id: Int, method: String, args: String) {
            pool.execute {
                val r = try { mobile.Mobile.call(method, args) } catch (e: Throwable) { "null" }
                js("window.__mmResolve($id, ${JSONObject.quote(r)})")
            }
        }
    }

    // Close SSH sessions while the app is not visible: no traffic in background.
    override fun onStop() {
        super.onStop()
        paused = true
        pool.execute { mobile.Mobile.pause() }
    }

    override fun onStart() {
        super.onStart()
        if (paused) {
            paused = false
            pool.execute { mobile.Mobile.resume() }
        }
    }

    @Deprecated("Deprecated in Java")
    override fun onBackPressed() {
        web.evaluateJavascript("(function(){var s=document.body.classList.contains('sheeting');if(s){document.dispatchEvent(new KeyboardEvent('keydown',{key:'Escape'}));}return s;})()") { r ->
            if (r != "true") super.onBackPressed()
        }
    }
}

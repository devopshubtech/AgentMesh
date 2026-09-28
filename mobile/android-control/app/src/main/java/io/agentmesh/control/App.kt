package io.agentmesh.control

import android.app.Application
import io.agentmesh.control.data.Api
import io.agentmesh.control.data.SecureStore

class App : Application() {
    lateinit var store: SecureStore
        private set
    lateinit var api: Api
        private set

    override fun onCreate() {
        super.onCreate()
        store = SecureStore(this)
        api = Api(store)
    }
}

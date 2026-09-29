package io.agentmesh.control

import android.app.Application
import io.agentmesh.control.data.ConnectApi
import io.agentmesh.control.data.ProfileStore

class App : Application() {
    lateinit var profiles: ProfileStore
        private set
    lateinit var connect: ConnectApi
        private set

    override fun onCreate() {
        super.onCreate()
        profiles = ProfileStore(this)
        connect = ConnectApi(profiles)
    }
}

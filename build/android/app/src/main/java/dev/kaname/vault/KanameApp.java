package dev.kaname.vault;

import android.app.Activity;
import android.app.Application;
import android.content.Context;
import android.os.Bundle;
import android.view.WindowManager;

import androidx.fragment.app.FragmentActivity;

import java.lang.ref.WeakReference;

/**
 * La Application de Kaname. Existe por dos cosas que el vault necesita: un
 * Context que no dependa de ninguna activity, para las SharedPreferences, y
 * saber cuál es la activity en primer plano, que es lo que el BiometricPrompt
 * necesita para mostrarse. Se declara en el manifest; MainActivity y el bridge
 * siguen siendo los de Wails, sin tocar.
 */
public final class KanameApp extends Application {

    private static volatile Context app;
    private static volatile WeakReference<FragmentActivity> current = new WeakReference<>(null);

    /** El Context de la aplicación; null solo si onCreate no corrió. */
    static Context appContext() {
        return app;
    }

    /**
     * La última activity que llegó a primer plano y sigue viva, o null. Se
     * retiene hasta onActivityDestroyed y no hasta onActivityPaused: mostrar el
     * BiometricPrompt pausa la activity en algunos equipos, y dos lecturas
     * seguidas —la contraseña de la base y la del bastión— llegarían con esto
     * en null entre el primer prompt y el resume.
     */
    static FragmentActivity currentActivity() {
        FragmentActivity a = current.get();
        if (a == null || a.isFinishing() || a.isDestroyed()) {
            return null;
        }
        return a;
    }

    @Override
    public void onCreate() {
        super.onCreate();
        app = getApplicationContext();
        registerActivityLifecycleCallbacks(new ActivityLifecycleCallbacks() {
            @Override public void onActivityResumed(Activity a) {
                if (a instanceof FragmentActivity) {
                    current = new WeakReference<>((FragmentActivity) a);
                }
            }
            @Override public void onActivityDestroyed(Activity a) {
                if (current.get() == a) {
                    current = new WeakReference<>(null);
                }
            }
            @Override public void onActivityCreated(Activity a, Bundle b) {
                // FLAG_SECURE: la ventana no sale en capturas, grabaciones ni
                // en la miniatura del selector de apps. Lo que hay en pantalla
                // son filas de una base; en el tren, la pantalla es pública.
                a.getWindow().setFlags(WindowManager.LayoutParams.FLAG_SECURE,
                        WindowManager.LayoutParams.FLAG_SECURE);
            }
            @Override public void onActivityStarted(Activity a) {}
            @Override public void onActivityPaused(Activity a) {}
            @Override public void onActivityStopped(Activity a) {}
            @Override public void onActivitySaveInstanceState(Activity a, Bundle b) {}
        });
    }
}

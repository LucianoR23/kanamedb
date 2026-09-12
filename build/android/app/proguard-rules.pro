# Add project specific ProGuard rules here.
# You can control the set of applied configuration files using the
# proguardFiles setting in build.gradle.

# Keep native methods
-keepclasseswithmembernames class * {
    native <methods>;
}

# Keep Wails bridge classes
-keep class com.wails.app.WailsBridge { *; }
-keep class com.wails.app.WailsJSBridge { *; }

# El vault de Kaname lo resuelve Go por FindClass desde JNI_OnLoad y llama a
# sus estáticos por nombre: nadie lo referencia desde Java. Sin esto, activar
# minify lo renombra y cada operación con contraseñas falla en tiempo de
# ejecución.
-keep class dev.kaname.vault.KanameVault { *; }
-keep class dev.kaname.vault.KanameVault$VaultException { *; }
-keep class dev.kaname.vault.KanameApp { *; }

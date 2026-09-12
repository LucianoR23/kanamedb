package dev.kaname.vault;

import android.content.Context;
import android.content.SharedPreferences;
import android.os.Looper;
import android.security.keystore.KeyGenParameterSpec;
import android.security.keystore.KeyPermanentlyInvalidatedException;
import android.security.keystore.KeyProperties;
import android.security.keystore.StrongBoxUnavailableException;
import android.util.Base64;

import androidx.biometric.BiometricManager;
import androidx.biometric.BiometricPrompt;
import androidx.core.content.ContextCompat;
import androidx.fragment.app.FragmentActivity;

import java.nio.charset.StandardCharsets;
import java.security.KeyStore;
import java.util.Arrays;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicReference;

import javax.crypto.Cipher;
import javax.crypto.KeyGenerator;
import javax.crypto.SecretKey;
import javax.crypto.spec.GCMParameterSpec;

/**
 * Donde viven las contraseñas en Android.
 *
 * Una clave AES-256 en el Android Keystore —en StrongBox si el equipo lo
 * tiene— creada con autenticación de usuario obligatoria por uso y atada a
 * biometría fuerte. Cada cifrado y cada descifrado pasan por un
 * BiometricPrompt con CryptoObject: el sistema no habilita el Cipher si la
 * persona no pasó. Lo que queda en disco (SharedPreferences propias) es
 * IV + texto cifrado en base64, con la clave de la entrada como AAD para que
 * un blob no se pueda mover de una conexión a otra.
 *
 * Lo llama Go por JNI desde un hilo del pool de bindings; nunca desde el
 * principal, porque espera el resultado del prompt. Todo entra y sale como
 * byte[] en UTF-8, para no depender del UTF-8 modificado de NewStringUTF.
 *
 * Lo que esto no cubre —un teléfono rooteado, un dedo forzado— está en
 * kaname-android.md.
 */
public final class KanameVault {

    private static final String KEYSTORE = "AndroidKeyStore";
    private static final String KEY_ALIAS = "kaname-vault-v1";
    private static final String PREFS = "kaname_vault";
    private static final String TRANSFORMATION = "AES/GCM/NoPadding";
    private static final int GCM_TAG_BITS = 128;
    private static final long PROMPT_TIMEOUT_SECONDS = 120;

    private KanameVault() {}

    /** Error con un mensaje para una persona; Go lo envuelve con el id de la conexión. */
    public static final class VaultException extends Exception {
        VaultException(String message) { super(message); }
        VaultException(String message, Throwable cause) { super(message, cause); }
    }

    // ---- API que llama Go ---------------------------------------------------

    /** Devuelve el secreto, o null si no hay nada guardado bajo esa clave. */
    public static byte[] get(byte[] key) throws VaultException {
        String k = utf8(key);
        String stored = prefs().getString(k, null);
        if (stored == null) {
            return null;
        }
        String[] parts = stored.split(":", 2);
        if (parts.length != 2) {
            throw new VaultException("La entrada guardada está corrupta.");
        }
        byte[] iv = Base64.decode(parts[0], Base64.NO_WRAP);
        byte[] ciphertext = Base64.decode(parts[1], Base64.NO_WRAP);

        SecretKey secret = loadKey();
        if (secret == null) {
            // Hay texto cifrado pero la clave ya no está: no se puede recuperar.
            wipe();
            throw new VaultException("La clave que protegía las contraseñas ya no está en el equipo. Se borraron; cargalas de nuevo.");
        }
        Cipher cipher = cipher();
        try {
            cipher.init(Cipher.DECRYPT_MODE, secret, new GCMParameterSpec(GCM_TAG_BITS, iv));
        } catch (KeyPermanentlyInvalidatedException e) {
            wipe();
            throw new VaultException("La biometría del equipo cambió y las contraseñas guardadas ya no se pueden descifrar. Se borraron; cargalas de nuevo.", e);
        } catch (Exception e) {
            throw new VaultException("No se pudo preparar el descifrado: " + e.getMessage(), e);
        }
        cipher = authenticate(cipher, "Leer la contraseña guardada");
        try {
            cipher.updateAAD(key);
            return cipher.doFinal(ciphertext);
        } catch (Exception e) {
            throw new VaultException("No se pudo descifrar la contraseña guardada; volvé a cargarla.", e);
        }
    }

    /** Guarda el secreto, reemplazando el anterior si había. */
    public static void set(byte[] key, byte[] value) throws VaultException {
        try {
            guardar(key, value);
        } finally {
            // En todos los caminos de salida, también si la persona canceló el
            // prompt: el texto en claro no se queda esperando al GC.
            Arrays.fill(value, (byte) 0);
        }
    }

    private static void guardar(byte[] key, byte[] value) throws VaultException {
        SecretKey secret = loadKey();
        if (secret == null) {
            // Sin clave, lo que hubiera cifrado no lo va a poder leer nadie
            // —el sistema la borró al quitar el bloqueo de pantalla, o es la
            // primera vez—. Se limpia antes de crear la nueva para que no
            // queden entradas que digan «hay contraseña» y no se puedan abrir.
            wipe();
            secret = createKey();
        }
        Cipher cipher = cipher();
        try {
            cipher.init(Cipher.ENCRYPT_MODE, secret);
        } catch (KeyPermanentlyInvalidatedException e) {
            // Igual que en get: se borra todo y se avisa. No se rehace la
            // clave en silencio, porque «guardado» taparía que el resto de las
            // contraseñas acaba de desaparecer. El próximo intento la crea.
            wipe();
            throw new VaultException("La biometría del equipo cambió y las contraseñas guardadas ya no se pueden descifrar. Se borraron; cargalas de nuevo, esta también.", e);
        } catch (Exception e) {
            throw new VaultException("No se pudo preparar el cifrado: " + e.getMessage(), e);
        }
        cipher = authenticate(cipher, "Guardar la contraseña");
        byte[] ciphertext;
        try {
            cipher.updateAAD(key);
            ciphertext = cipher.doFinal(value);
        } catch (Exception e) {
            throw new VaultException("No se pudo cifrar la contraseña.", e);
        }
        String stored = Base64.encodeToString(cipher.getIV(), Base64.NO_WRAP)
                + ":" + Base64.encodeToString(ciphertext, Base64.NO_WRAP);
        if (!prefs().edit().putString(utf8(key), stored).commit()) {
            throw new VaultException("No se pudo escribir la contraseña cifrada en el almacenamiento de la app.");
        }
    }

    /** Borra la entrada. Borrar lo que no está no es un error. */
    public static void delete(byte[] key) throws VaultException {
        if (!prefs().edit().remove(utf8(key)).commit()) {
            throw new VaultException("No se pudo borrar la contraseña del almacenamiento de la app.");
        }
    }

    /** Dice si hay algo guardado, sin descifrar y sin pedir biometría. */
    public static boolean has(byte[] key) throws VaultException {
        return prefs().contains(utf8(key));
    }

    // ---- Keystore ---------------------------------------------------------

    private static SecretKey loadKey() throws VaultException {
        try {
            KeyStore ks = KeyStore.getInstance(KEYSTORE);
            ks.load(null);
            KeyStore.Entry entry = ks.getEntry(KEY_ALIAS, null);
            if (entry instanceof KeyStore.SecretKeyEntry) {
                return ((KeyStore.SecretKeyEntry) entry).getSecretKey();
            }
            return null;
        } catch (Exception e) {
            throw new VaultException("No se pudo abrir el Keystore del equipo: " + e.getMessage(), e);
        }
    }

    private static SecretKey createKey() throws VaultException {
        try {
            return generate(true);
        } catch (StrongBoxUnavailableException e) {
            // Sin StrongBox la clave igual queda en el TEE; es lo que hay.
            try {
                return generate(false);
            } catch (Exception e2) {
                throw new VaultException("No se pudo crear la clave en el Keystore: " + e2.getMessage(), e2);
            }
        } catch (Exception e) {
            throw new VaultException("No se pudo crear la clave en el Keystore: " + e.getMessage(), e);
        }
    }

    private static SecretKey generate(boolean strongBox) throws Exception {
        KeyGenParameterSpec.Builder b = new KeyGenParameterSpec.Builder(
                KEY_ALIAS, KeyProperties.PURPOSE_ENCRYPT | KeyProperties.PURPOSE_DECRYPT)
                .setBlockModes(KeyProperties.BLOCK_MODE_GCM)
                .setEncryptionPaddings(KeyProperties.ENCRYPTION_PADDING_NONE)
                .setKeySize(256)
                // Por uso (timeout 0): cada init del Cipher exige pasar por el
                // prompt, con CryptoObject. Solo biometría fuerte; el PIN del
                // equipo no vale como respaldo.
                .setUserAuthenticationRequired(true)
                .setUserAuthenticationParameters(0, KeyProperties.AUTH_BIOMETRIC_STRONG)
                // Una huella nueva invalida la clave: lo guardado deja de
                // poder leerse, a propósito.
                .setInvalidatedByBiometricEnrollment(true)
                .setIsStrongBoxBacked(strongBox);
        KeyGenerator kg = KeyGenerator.getInstance(KeyProperties.KEY_ALGORITHM_AES, KEYSTORE);
        kg.init(b.build());
        return kg.generateKey();
    }

    /** Borra la clave y todo lo cifrado con ella. */
    private static void wipe() {
        try {
            prefs().edit().clear().commit();
        } catch (VaultException ignored) {
            // Sin Context no hay prefs que borrar.
        }
        try {
            KeyStore ks = KeyStore.getInstance(KEYSTORE);
            ks.load(null);
            ks.deleteEntry(KEY_ALIAS);
        } catch (Exception ignored) {
            // Si no se pudo borrar, la próxima createKey la pisa.
        }
    }

    private static Cipher cipher() throws VaultException {
        try {
            return Cipher.getInstance(TRANSFORMATION);
        } catch (Exception e) {
            throw new VaultException("El equipo no ofrece AES-GCM: " + e.getMessage(), e);
        }
    }

    // ---- Biometría --------------------------------------------------------

    /**
     * Muestra el BiometricPrompt con el Cipher como CryptoObject y espera.
     * Devuelve el Cipher ya autorizado por el sistema.
     */
    private static Cipher authenticate(Cipher cipher, String subtitle) throws VaultException {
        if (Looper.getMainLooper().isCurrentThread()) {
            // Esperar acá bloquearía el hilo que tiene que dibujar el prompt.
            throw new VaultException("El acceso a las contraseñas no puede pedirse desde el hilo principal.");
        }
        final FragmentActivity activity = KanameApp.currentActivity();
        if (activity == null) {
            throw new VaultException("Kaname tiene que estar en primer plano para pedir la biometría.");
        }
        switch (BiometricManager.from(activity).canAuthenticate(BiometricManager.Authenticators.BIOMETRIC_STRONG)) {
            case BiometricManager.BIOMETRIC_SUCCESS:
                break;
            case BiometricManager.BIOMETRIC_ERROR_NO_HARDWARE:
            case BiometricManager.BIOMETRIC_ERROR_HW_UNAVAILABLE:
                throw new VaultException("Este equipo no tiene biometría fuerte disponible; Kaname no guarda contraseñas sin ella.");
            case BiometricManager.BIOMETRIC_ERROR_NONE_ENROLLED:
                throw new VaultException("No hay ninguna huella o rostro registrado en el equipo. Configuralo en Ajustes y volvé a intentar.");
            default:
                throw new VaultException("La biometría del equipo no está disponible en este momento.");
        }

        final CountDownLatch done = new CountDownLatch(1);
        final AtomicReference<Cipher> authorized = new AtomicReference<>();
        final AtomicReference<String> failure = new AtomicReference<>();
        final BiometricPrompt.PromptInfo info = new BiometricPrompt.PromptInfo.Builder()
                .setTitle("Kaname")
                .setSubtitle(subtitle)
                .setNegativeButtonText("Cancelar")
                .setAllowedAuthenticators(BiometricManager.Authenticators.BIOMETRIC_STRONG)
                .setConfirmationRequired(false)
                .build();

        activity.runOnUiThread(() -> {
            try {
                BiometricPrompt prompt = new BiometricPrompt(activity, ContextCompat.getMainExecutor(activity),
                        new BiometricPrompt.AuthenticationCallback() {
                            @Override
                            public void onAuthenticationSucceeded(BiometricPrompt.AuthenticationResult result) {
                                BiometricPrompt.CryptoObject co = result.getCryptoObject();
                                if (co == null || co.getCipher() == null) {
                                    failure.set("El sistema autenticó pero no habilitó la clave.");
                                } else {
                                    authorized.set(co.getCipher());
                                }
                                done.countDown();
                            }

                            @Override
                            public void onAuthenticationError(int code, CharSequence msg) {
                                if (code == BiometricPrompt.ERROR_NEGATIVE_BUTTON || code == BiometricPrompt.ERROR_USER_CANCELED) {
                                    failure.set("Cancelado.");
                                } else {
                                    failure.set("Biometría: " + msg);
                                }
                                done.countDown();
                            }

                            @Override
                            public void onAuthenticationFailed() {
                                // Un intento fallido: el prompt sigue abierto y deja reintentar.
                            }
                        });
                prompt.authenticate(info, new BiometricPrompt.CryptoObject(cipher));
            } catch (Exception e) {
                failure.set("No se pudo mostrar el pedido de biometría: " + e.getMessage());
                done.countDown();
            }
        });

        try {
            if (!done.await(PROMPT_TIMEOUT_SECONDS, TimeUnit.SECONDS)) {
                throw new VaultException("Se agotó el tiempo esperando la biometría.");
            }
        } catch (InterruptedException e) {
            Thread.currentThread().interrupt();
            throw new VaultException("Interrumpido esperando la biometría.", e);
        }
        if (failure.get() != null) {
            throw new VaultException(failure.get());
        }
        return authorized.get();
    }

    // ---- Utilidades -------------------------------------------------------

    private static SharedPreferences prefs() throws VaultException {
        Context ctx = KanameApp.appContext();
        if (ctx == null) {
            throw new VaultException("La aplicación no terminó de iniciarse.");
        }
        return ctx.getSharedPreferences(PREFS, Context.MODE_PRIVATE);
    }

    private static String utf8(byte[] b) {
        return new String(b, StandardCharsets.UTF_8);
    }
}

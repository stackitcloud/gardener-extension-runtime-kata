<p>Packages:</p>
<ul>
<li>
<a href="#kata.runtime.extensions.config.gardener.cloud%2fv1alpha1">kata.runtime.extensions.config.gardener.cloud/v1alpha1</a>
</li>
</ul>

<h2 id="kata.runtime.extensions.config.gardener.cloud/v1alpha1">kata.runtime.extensions.config.gardener.cloud/v1alpha1</h2>
<p>

</p>

<h3 id="kataconfiguration">KataConfiguration
</h3>


<p>
KataConfiguration defines the provider configuration for the Kata Containers runtime extension.

It is set as `providerConfig` on a worker pool's ContainerRuntime of type `kata`. It is
intentionally empty for now: the installed RuntimeClasses (kata-qemu, kata-clh) fully determine
the node behaviour. The type is kept so that runtime-specific configuration can be added later
without breaking the API contract.
</p>


